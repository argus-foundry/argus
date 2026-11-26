package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Webhook posts a short JSON body to a loopback URL when a session needs
// input or a task completes. Delivery is best-effort and never blocks Emit.
type Webhook struct {
	urlOf  func() string
	nameOf func(string) string
	client *http.Client

	ch       chan model.Event
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	unsub    func() // guarded by mu, non-nil once started
	stopOnce sync.Once
	dropped  atomic.Uint64

	badMu  sync.Mutex
	badURL string
}

// NewWebhook builds a notifier. urlOf is read on every event so a config
// change takes effect without a restart. nameOf may be nil.
func NewWebhook(urlOf func() string, nameOf func(string) string) *Webhook {
	if urlOf == nil {
		urlOf = func() string { return "" }
	}
	if nameOf == nil {
		nameOf = func(string) string { return "" }
	}
	// Stop owns cancel. G118 only sees calls in this function.
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: Stop calls cancel
	return &Webhook{
		urlOf: urlOf, nameOf: nameOf, client: newWebhookClient(),
		ch: make(chan model.Event, 32), ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
}

// Start subscribes to the event bus and delivers in the background.
func (w *Webhook) Start() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.unsub == nil && w.ctx.Err() == nil {
		w.unsub = events.Subscribe(w.enqueue)
		go w.loop()
	}
}

// Stop unsubscribes and waits for the delivery loop to finish.
func (w *Webhook) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.cancel()
		started := w.unsub != nil
		if w.unsub != nil {
			w.unsub()
		}
		w.mu.Unlock()
		if started {
			select {
			case <-w.done:
			case <-time.After(3 * time.Second):
			}
		}
	})
}

func (w *Webhook) enqueue(ev model.Event) {
	if _, ok := notifyKind(ev); !ok {
		return
	}
	select {
	case <-w.ctx.Done():
		return
	case w.ch <- ev:
	default:
		w.dropped.Add(1)
	}
}

func (w *Webhook) loop() {
	defer close(w.done)
	for w.ctx.Err() == nil {
		select {
		case <-w.ctx.Done():
			return
		case ev := <-w.ch:
			w.deliver(ev)
			if dropped := w.dropped.Swap(0); dropped > 0 {
				slog.Warn("[voice] webhook queue full", "dropped", dropped)
				uxlog.Log("[voice] webhook queue full dropped=%d", dropped)
			}
		}
	}
}

func (w *Webhook) deliver(ev model.Event) {
	kind, ok := notifyKind(ev)
	if !ok {
		return
	}
	raw := strings.TrimSpace(w.urlOf())
	if raw == "" {
		return
	}
	if err := ValidateWebhookURL(raw); err != nil {
		w.logBadURL(raw, err)
		return
	}
	name := strings.TrimSpace(w.nameOf(ev.TaskID))
	var speech string
	switch kind {
	case "needs_input":
		speech = sentence(SpeakName(name) + " needs your input")
	default:
		speech = sentence(SpeakName(name) + " is finished")
	}
	body, err := json.Marshal(map[string]any{
		"type":      ev.Type,
		"task_id":   ev.TaskID,
		"task_name": name,
		"speech":    speech,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(body))
	if err != nil {
		slog.Warn("[voice] webhook request could not be constructed")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		slog.Warn("[voice] webhook post failed", "type", ev.Type, "task", ev.TaskID, "err", err)
		uxlog.Log("[voice] webhook failed type=%s task=%s err=%v", ev.Type, ev.TaskID, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("[voice] webhook post failed", "type", ev.Type, "task", ev.TaskID, "status", resp.StatusCode)
		uxlog.Log("[voice] webhook failed type=%s task=%s status=%d", ev.Type, ev.TaskID, resp.StatusCode)
		return
	}
	slog.Info("[voice] webhook posted", "type", ev.Type, "task", ev.TaskID)
	uxlog.Log("[voice] webhook posted type=%s task=%s", ev.Type, ev.TaskID)
}

func (w *Webhook) logBadURL(raw string, err error) {
	w.badMu.Lock()
	defer w.badMu.Unlock()
	if w.badURL == raw {
		return
	}
	w.badURL = raw
	slog.Warn("[voice] webhook url rejected", "err", err)
	uxlog.Log("[voice] webhook url rejected err=%v", err)
}

// ValidateWebhookURL accepts only absolute http or https URLs whose host is
// localhost or a loopback address. An empty string is not a URL.
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("voice webhook url must be an absolute http or https url")
	}
	if u.User != nil {
		return errors.New("voice webhook url must not carry credentials")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return errors.New("voice webhook url must use a loopback host")
		}
		return nil
	}
	if !strings.EqualFold(host, "localhost") {
		return errors.New("voice webhook url must use a loopback host")
	}
	return nil
}

func notifyKind(ev model.Event) (string, bool) {
	switch ev.Type {
	case model.EventTypeSessionNeedsInput, model.EventTypeResidentPrefix + model.EventTypeSessionNeedsInput:
		var body struct {
			NeedsInput bool `json:"needs_input"`
		}
		if json.Unmarshal(ev.Payload, &body) != nil || !body.NeedsInput {
			return "", false
		}
		return "needs_input", true
	case model.EventTypeTaskCompleted, model.EventTypeResidentPrefix + model.EventTypeTaskCompleted:
		return "completed", true
	default:
		return "", false
	}
}

func newWebhookClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("voice webhook redirects are not followed")
		},
		Transport: &http.Transport{
			Proxy:       func(*http.Request) (*url.URL, error) { return nil, nil },
			DialContext: dialLoopback,
		},
	}
}

func dialLoopback(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, r := range resolved {
			ips = append(ips, r.IP)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("voice webhook host did not resolve")
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return nil, errors.New("voice webhook host is not loopback")
		}
	}
	var d net.Dialer
	for _, ip := range ips {
		var conn net.Conn
		conn, err = d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}
