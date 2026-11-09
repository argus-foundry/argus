package api

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/skills"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskContext_CodexCaptureMainThread(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	main, child := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	e.addThread(t, main, "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100))
	e.addThread(t, child, "/w/cx", 2, codexTokenLine("2026-11-07T10:00:01Z", 90, 0, 100))
	conn, err := sql.Open("sqlite", e.stateD)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`UPDATE threads SET source='{"subagent":{}}' WHERE id=?`, child)
	testutil.NoError(t, err)
	_, before := e.get(t)
	eqAny(t, before["session_id"], main)
	captured, err := agent.CaptureSessionID(e.task, e.s.db.Config())
	testutil.NoError(t, err)
	testutil.Equal(t, captured, main)
	e.task.SessionID = captured
	testutil.NoError(t, e.s.db.Update(e.task))
	_, after := e.get(t)
	eqAny(t, after["session_id"], main)
	testutil.NotNil(t, after["context"])
}

func TestTaskContext_CodexIndexFIFO(t *testing.T) {
	e := newCodexCtxEnv(t, "own")
	e.run(false)
	testutil.NoError(t, os.Remove(e.stateD))
	testutil.NoError(t, syscall.Mkfifo(e.stateD, 0o600))
	done := make(chan struct{})
	go func() { e.s.idleWatcherTick(newIdleWatcherState()); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		writer, err := os.OpenFile(e.stateD, os.O_RDWR|syscall.O_NONBLOCK, 0)
		testutil.NoError(t, err)
		testutil.NoError(t, writer.Close())
		<-done
		t.Fatal("index FIFO stalled idle watcher until a writer arrived")
	}
}

func TestTaskContext_CodexFreshOverlayStorage(t *testing.T) {
	for _, dir := range []string{"sessions", "archived_sessions"} {
		t.Run(dir, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			e := newCodexCtxEnv(t, "own")
			testutil.NoError(t, os.Remove(filepath.Join(e.dir, "sessions")))
			overlay, err := skills.ArgusCodexHomeFor(e.dir)
			testutil.NoError(t, err)
			testutil.NoError(t, os.MkdirAll(filepath.Join(overlay, dir), 0o700))
			path := filepath.Join(overlay, dir, "rollout-own.jsonl")
			testutil.NoError(t, os.WriteFile(path, []byte(codexSessionMeta("own", "/w/cx", "2026-11-07T10:00:00Z")+codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100)), 0o600))
			conn, err := sql.Open("sqlite", e.stateD)
			testutil.NoError(t, err)
			defer conn.Close() //nolint:errcheck // test handle
			_, err = conn.Exec(`INSERT INTO threads VALUES ('own','/w/cx',?,1,1,'cli')`, path)
			testutil.NoError(t, err)
			_, body := e.get(t)
			testutil.NotNil(t, body["context"])
		})
	}
}

func TestTaskContext_CodexFreshTokensDoNotRenewOldLaunch(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.run(false)
	e.addThread(t, "old", "/w/cx", 1, codexTokenLine(time.Now().UTC().Format(time.RFC3339Nano), 10, 0, 100))
	_, body := e.get(t)
	eqAny(t, body["live"], false)
	testutil.Nil(t, body["context"])
}

func TestTaskContext_CodexForeignOverlayRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	e := newCodexCtxEnv(t, "own")
	e.addThread(t, "own", "/w/cx", 1, "")
	foreign, err := skills.ArgusCodexHomeFor(t.TempDir())
	testutil.NoError(t, err)
	testutil.NoError(t, os.MkdirAll(filepath.Join(foreign, "sessions"), 0o700))
	path := filepath.Join(foreign, "sessions", "rollout-own.jsonl")
	testutil.NoError(t, os.WriteFile(path, []byte(codexSessionMeta("own", "/w/cx", "2026-11-07T10:00:00Z")+codexTokenLine("2026-11-07T10:00:01Z", 777, 0, 1000)), 0o600))
	conn, err := sql.Open("sqlite", e.stateD)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`UPDATE threads SET rollout_path=?`, path)
	testutil.NoError(t, err)
	_, body := e.get(t)
	testutil.Nil(t, body["context"])
}
