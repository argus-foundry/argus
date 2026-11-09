package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// capturedLog is a minimal slog handler recording rendered messages. It is
// mutex-guarded: the global default logger also receives records from other
// tests' still-running server goroutines.
type capturedLog struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func newCapturedLog() capturedLog { return capturedLog{mu: &sync.Mutex{}, buf: &bytes.Buffer{}} }

func (h capturedLog) Enabled(context.Context, slog.Level) bool { return true }
func (h capturedLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.WriteString(r.Message + "\n")
	return nil
}
func (h capturedLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h capturedLog) WithGroup(string) slog.Handler      { return h }
func (h capturedLog) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buf.String()
}

func TestResidentWaitingChangeReachesSlog(t *testing.T) {
	logs := newCapturedLog()
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv, d := testServer(t)
	close(srv.stopCh)
	r, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	state := newIdleWatcherState()
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, func(string) []byte { return blockedTail })
	testutil.Equal(t, srv.ResidentWaiting(), true)
	testutil.Contains(t, logs.String(), "[resident] waiting changed")
}

// TestIdleWatcherTick_ResidentSkipsPush drives a resident and an ordinary
// task through busy -> idle with a real push manager whose single
// subscription points at a counting httptest endpoint. Exactly one push (the
// ordinary task's) may fire, while both transitions still emit session.idle
// (the resident's rewritten into the resident.* namespace).
func TestIdleWatcherTick_ResidentSkipsPush(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real PTY-backed sessions and waits for the idle threshold")
	}
	t.Setenv("HOME", t.TempDir()) // ResidentWorkDir resolves under the data dir
	srv, d, pm := pushTestServer(t)

	var hits atomic.Int32
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(ep.Close)
	_, p256dh, err := webpush.GenerateVAPIDKeys()
	testutil.NoError(t, err)
	authSecret := make([]byte, 16) // RFC 8291 auth secret, generated per run
	_, err = rand.Read(authSecret)
	testutil.NoError(t, err)
	_, err = d.AddPushSubscription(db.PushSubscription{Label: "t", Endpoint: ep.URL, P256dh: p256dh, Auth: base64.RawURLEncoding.EncodeToString(authSecret)})
	testutil.NoError(t, err)
	testutil.NoError(t, pm.SetSubject("https://example.test"))

	sink := &recordingSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })

	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	testutil.NoError(t, d.SetBackend("sh-echo", config.Backend{Command: "sh -c 'echo hi; sleep 30'"}))
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	res.Backend = "sh-echo"
	testutil.NoError(t, d.Update(res))
	plain := &model.Task{Name: "plain-idle", Status: model.StatusInProgress, Backend: "sh-echo", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(plain))

	for _, task := range []*model.Task{res, plain} {
		sess, err := srv.runner.Start(task, d.Config(), 24, 80, false)
		if err != nil {
			t.Fatalf("start %s: %v", task.Name, err)
		}
		id := task.ID
		t.Cleanup(func() { _ = srv.runner.Stop(id); <-sess.Done() })
	}

	state := newIdleWatcherState()
	srv.idleWatcherTick(state) // baseline: records busy state
	for _, task := range []*model.Task{res, plain} {
		_, err := srv.runner.Get(task.ID).WriteInput([]byte("x"), agentview.OriginUser)
		testutil.NoError(t, err)
	}
	// Let the session pass the raw-idle threshold, then tick for the transition.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && hits.Load() == 0 {
		time.Sleep(500 * time.Millisecond)
		srv.idleWatcherTick(state)
	}
	// Allow any (wrongly) fired second push to land.
	time.Sleep(500 * time.Millisecond)
	testutil.Equal(t, hits.Load(), int32(1))

	types := map[string]string{}
	for _, ev := range sink.events() {
		if ev.Type == model.EventTypeSessionIdle || ev.Type == model.EventTypeResidentPrefix+model.EventTypeSessionIdle {
			types[ev.TaskID] = ev.Type
		}
	}
	testutil.Equal(t, types[plain.ID], model.EventTypeSessionIdle)
	testutil.Equal(t, types[res.ID], "resident.session.idle")
}
