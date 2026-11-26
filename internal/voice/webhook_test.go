package voice

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestValidateWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		ok        bool
	}{
		{"loopback", "http://127.0.0.1:9/hook", true},
		{"localhost", "http://localhost:9/hook", true},
		{"ipv6", "http://[::1]:9/hook", true},
		{"public", "https://example.com/hook", false},
		{"metadata", "http://169.254.169.254/", false},
		{"any", "http://0.0.0.0:9/", false},
		{"creds", "http://user:pass@127.0.0.1/", false},
		{"file", "file:///tmp/x", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWebhookURL(tc.raw)
			if tc.ok {
				testutil.NoError(t, err)
			} else {
				testutil.Error(t, err)
			}
		})
	}
}

func TestWebhookPostsNeedsInputAndCompleted(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		testutil.NoError(t, json.Unmarshal(raw, &body))
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	hook := NewWebhook(func() string { return srv.URL }, func(id string) string {
		if id == "t1" {
			return "Review"
		}
		return "Docs"
	})
	hook.Start()
	t.Cleanup(hook.Stop)

	events.Emit(model.EventTypeSessionNeedsInput, "t1", map[string]bool{"needs_input": false})
	events.Emit(model.EventTypeSessionNeedsInput, "t1", map[string]bool{"needs_input": true})
	events.Emit(model.EventTypeTaskCompleted, "t2", nil)
	events.Emit(model.EventTypeTaskUpdated, "t2", nil)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	testutil.Equal(t, len(bodies), 2)
	testutil.Equal(t, bodies[0]["speech"], "Review needs your input.")
	testutil.Equal(t, bodies[0]["type"], model.EventTypeSessionNeedsInput)
	testutil.Equal(t, bodies[1]["speech"], "Docs is finished.")
	testutil.Equal(t, bodies[1]["type"], model.EventTypeTaskCompleted)
}

type webhookTransportFunc func(*http.Request) (*http.Response, error)

func (f webhookTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebhookSkipsEmptyAndPublicURL(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"empty", ""},
		{"public", "https://example.com/hook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := NewWebhook(func() string { return tc.raw }, nil)
			t.Cleanup(hook.Stop)
			calls := 0
			hook.client.Transport = webhookTransportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, context.Canceled
			})
			hook.deliver(model.Event{Type: model.EventTypeTaskCompleted, TaskID: "t"})
			testutil.Equal(t, calls, 0)
		})
	}
}

func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	var first, second atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		second.Add(1)
	}))
	t.Cleanup(target.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			first.Add(1)
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	hook := NewWebhook(func() string { return srv.URL }, func(string) string { return "Review" })
	t.Cleanup(hook.Stop)
	hook.deliver(model.Event{Type: model.EventTypeTaskCompleted, TaskID: "t"})
	testutil.Equal(t, first.Load(), int32(1))
	testutil.Equal(t, second.Load(), int32(0))
}

func TestWebhookStatusAndNilReceiver(t *testing.T) {
	var status atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status.Store(int32(http.StatusInternalServerError))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	hook := NewWebhook(nil, nil)
	hook.urlOf = func() string { return srv.URL }
	hook.Start()
	events.Emit(model.EventTypeTaskCompleted, "t", nil)
	time.Sleep(50 * time.Millisecond)
	hook.Stop()
	hook.Stop()
	hook.enqueue(model.Event{Type: model.EventTypeTaskCompleted, TaskID: "t"})
	(*Webhook)(nil).Start()
	(*Webhook)(nil).Stop()
	testutil.Equal(t, status.Load(), int32(http.StatusInternalServerError))
}

func TestWebhookDropsWhenQueueIsFull(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-hold
	}))
	t.Cleanup(srv.Close)
	hook := NewWebhook(func() string { return srv.URL }, func(string) string { return "Review" })
	hook.Start()
	t.Cleanup(func() {
		close(hold)
		hook.Stop()
	})
	events.Emit(model.EventTypeTaskCompleted, "blocked", nil)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("webhook request never reached the receiver")
	}
	for i := 0; i < cap(hook.ch)+1; i++ {
		hook.enqueue(model.Event{Type: model.EventTypeTaskCompleted, TaskID: "q"})
	}
	testutil.Equal(t, hook.dropped.Load(), uint64(1))
}

func TestDialLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	testutil.NoError(t, err)
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("listener close: %v", err)
		}
	})
	conn, err := dialLoopback(context.Background(), "tcp", ln.Addr().String())
	testutil.NoError(t, err)
	conn.Close()

	_, err = dialLoopback(context.Background(), "tcp", "not-a-host")
	testutil.Error(t, err)
	_, err = dialLoopback(context.Background(), "tcp", "1.2.3.4:1")
	testutil.Error(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = dialLoopback(ctx, "tcp", "no-such-host.invalid:80")
	testutil.Error(t, err)
}
