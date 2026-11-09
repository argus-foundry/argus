package api

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskContext_CodexRoundingAndPending(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 12345, 0, 200000))
	e.run(false)
	_, body := e.get(t)
	eqAny(t, body["live"], true)
	eqAny(t, body["context"].(map[string]any)["percent"], float64(6.2))
	e.s.runner.(*contextTestRunner).pending = true
	_, body = e.get(t)
	eqAny(t, body["live"], false)
}

func TestTaskContext_CodexWatcherWiring(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100))
	e.run(false)
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	state := newIdleWatcherState()
	e.s.idleWatcherTick(state)
	e.s.idleWatcherTick(state)
	evs := sink.ofType(model.EventTypeTaskContextChanged)
	testutil.Equal(t, len(evs), 1)
	if len(evs) == 1 {
		testutil.Equal(t, evs[0].TaskID, e.task.ID)
		testutil.Equal(t, string(evs[0].Payload), `{"measured_at":"2026-11-07T10:00:01Z","session_id":"thr1"}`)
	}
}

func TestTaskContext_CodexStorageAliases(t *testing.T) {
	for _, dir := range []string{"sessions", "archived_sessions"} {
		t.Run(dir, func(t *testing.T) {
			e := newCodexCtxEnv(t, "thr1")
			p := e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100))
			storage := t.TempDir()
			dest := filepath.Join(storage, filepath.Base(p))
			testutil.NoError(t, os.Rename(p, dest))
			if dir == "sessions" {
				testutil.NoError(t, os.Remove(filepath.Join(e.dir, dir)))
			}
			testutil.NoError(t, os.Symlink(storage, filepath.Join(e.dir, dir)))
			conn, err := sql.Open("sqlite", e.stateD)
			testutil.NoError(t, err)
			defer conn.Close() //nolint:errcheck // test handle
			// The index may use the real source path or the overlay path.
			for _, path := range []string{dest, filepath.Join(e.dir, dir, filepath.Base(dest))} {
				_, err = conn.Exec(`UPDATE threads SET rollout_path=? WHERE id='thr1'`, path)
				testutil.NoError(t, err)
				_, body := e.get(t)
				testutil.NotNil(t, body["context"])
			}
		})
	}
}

func TestTaskContext_CodexFIFOThroughWatcher(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	p := e.addThread(t, "thr1", "/w/cx", 1, "")
	testutil.NoError(t, os.Remove(p))
	testutil.NoError(t, syscall.Mkfifo(p, 0o600))
	e.run(false)
	done := make(chan struct{})
	go func() { e.s.idleWatcherTick(newIdleWatcherState()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		writer, err := os.OpenFile(p, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			defer writer.Close() //nolint:errcheck // release blocked reader on regression
		}
		<-done
		t.Fatal("FIFO blocked the idle watcher")
	}
}
