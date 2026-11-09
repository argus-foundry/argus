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
	"path/filepath"
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

// TestIdleWatcherTick_ResidentSkipsPush drives a task through busy -> idle
// with a real push manager whose single subscription points at a counting
// httptest endpoint. Each case runs ALONE so a push cannot be attributed to
// the wrong task: the resident's transition emits resident.session.idle and
// fires no push, while a plain task's emits session.idle and fires exactly one.
func TestIdleWatcherTick_ResidentSkipsPush(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real PTY-backed sessions and waits for the idle threshold")
	}
	for _, tc := range []struct {
		name      string
		resident  bool
		eventType string
		wantHits  int32
	}{
		{"resident only", true, model.EventTypeResidentPrefix + model.EventTypeSessionIdle, 0},
		{"plain only", false, model.EventTypeSessionIdle, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

			testutil.NoError(t, d.SetBackend("sh-echo", config.Backend{Command: "sh -c 'echo hi; sleep 30'"}))
			// BuildCmd fails closed for a resident on a family that cannot
			// enforce its permission policy, so the resident runs a fake
			// binary named "claude" (flags ignored) instead of the sh backend.
			fakeDir := t.TempDir()
			fake := filepath.Join(fakeDir, "claude")
			testutil.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\necho hi\nsleep 30\n"), 0o755))
			testutil.NoError(t, d.SetBackend("fake-claude", config.Backend{Command: fake}))
			var task *model.Task
			if tc.resident {
				testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
				task, _, err = d.EnsureResidentTask(db.ResidentWorkDir())
				testutil.NoError(t, err)
				task.Backend = "fake-claude"
				testutil.NoError(t, d.Update(task))
			} else {
				task = &model.Task{Name: "plain-idle", Status: model.StatusInProgress, Backend: "sh-echo", Worktree: t.TempDir()}
				testutil.NoError(t, d.Add(task))
			}

			sess, err := srv.runner.Start(task, d.Config(), 24, 80, false)
			testutil.NoError(t, err)
			id := task.ID
			t.Cleanup(func() { _ = srv.runner.Stop(id); <-sess.Done() })

			state := newIdleWatcherState()
			srv.idleWatcherTick(state) // baseline: records busy state
			_, err = srv.runner.Get(id).WriteInput([]byte("x"), agentview.OriginUser)
			testutil.NoError(t, err)

			sawIdle := func() bool {
				for _, ev := range sink.events() {
					if ev.TaskID == id && ev.Type == tc.eventType {
						return true
					}
				}
				return false
			}
			// Tick until the idle transition is observed in the event stream.
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) && !sawIdle() {
				time.Sleep(250 * time.Millisecond)
				srv.idleWatcherTick(state)
			}
			if !sawIdle() {
				t.Fatalf("no %s event for the task within 10s", tc.eventType)
			}
			if tc.wantHits > 0 {
				for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && hits.Load() < tc.wantHits; {
					time.Sleep(50 * time.Millisecond)
				}
			}
			// Allow any (wrongly) fired extra push to land.
			time.Sleep(500 * time.Millisecond)
			testutil.Equal(t, hits.Load(), tc.wantHits)
		})
	}
}
