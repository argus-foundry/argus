package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/testutil"
)

func codexSessionMeta(id, cwd, at string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":%q}}`+"\n", at, id, cwd, at)
}

type contextTestSession struct{ *bridgeTestSession }

func (*contextTestSession) PID() int { return os.Getpid() }

type contextTestRunner struct {
	agent.SessionRunner
	id      string
	session agent.SessionHandle
	pending bool
}

func (r *contextTestRunner) Get(id string) agent.SessionHandle {
	if r.id == id {
		return r.session
	}
	return nil
}
func (r *contextTestRunner) RunningAndIdle() ([]string, []string) { return []string{r.id}, nil }
func (r *contextTestRunner) HasPendingRestart(string) bool        { return r.pending }

func (e *codexCtxEnv) run(pending bool) {
	e.s.runner = &contextTestRunner{SessionRunner: agent.NewRunner(nil), id: e.task.ID, session: &contextTestSession{&bridgeTestSession{}}, pending: pending}
}

func TestTaskContext_CodexReplacementLaunch(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "old", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 190000, 0, 200000))
	e.run(false)
	_, body := e.get(t)
	eqAny(t, body["live"], false)
	testutil.Nil(t, body["context"])
	// A new conversation from the running launch restores current context.
	at := time.Now().UTC().Format(time.RFC3339Nano)
	p := e.addThread(t, "new", "/w/cx", 2, "")
	testutil.NoError(t, os.WriteFile(p, []byte(codexSessionMeta("new", "/w/cx", at)+codexTokenLine(at, 10, 0, 100)), 0o600))
	_, body = e.get(t)
	eqAny(t, body["live"], true)
	if body["context"] != nil {
		eqAny(t, body["context"].(map[string]any)["used_tokens"], float64(10))
	}
}

func TestTaskContext_CodexForeignRollout(t *testing.T) {
	for _, kind := range []string{"direct path", "foreign path matching metadata", "symlink", "wrong session", "wrong worktree", "missing metadata"} {
		t.Run(kind, func(t *testing.T) {
			e := newCodexCtxEnv(t, "own")
			p := e.addThread(t, "own", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100))
			foreign := filepath.Join(t.TempDir(), "rollout-foreign.jsonl")
			testutil.NoError(t, os.WriteFile(foreign, []byte(codexSessionMeta("foreign", "/elsewhere", "2026-11-07T10:00:00Z")+codexTokenLine("2026-11-07T10:00:01Z", 777, 0, 1000)), 0o600))
			switch kind {
			case "direct path", "foreign path matching metadata":
				if kind == "foreign path matching metadata" {
					testutil.NoError(t, os.WriteFile(foreign, []byte(codexSessionMeta("own", "/w/cx", "2026-11-07T10:00:00Z")+codexTokenLine("2026-11-07T10:00:01Z", 777, 0, 1000)), 0o600))
				}
				conn, err := sql.Open("sqlite", e.stateD)
				testutil.NoError(t, err)
				defer conn.Close() //nolint:errcheck // test handle
				_, err = conn.Exec(`UPDATE threads SET rollout_path=? WHERE id='own'`, foreign)
				testutil.NoError(t, err)
			case "symlink":
				testutil.NoError(t, os.Remove(p))
				testutil.NoError(t, os.Symlink(foreign, p))
			default:
				id, cwd := "own", "/w/cx"
				if kind == "wrong session" {
					id = "foreign"
				}
				if kind == "wrong worktree" {
					cwd = "/elsewhere"
				}
				meta := codexSessionMeta(id, cwd, "2026-11-07T10:00:00Z")
				if kind == "missing metadata" {
					meta = ""
				}
				testutil.NoError(t, os.WriteFile(p, []byte(meta+codexTokenLine("2026-11-07T10:00:01Z", 777, 0, 1000)), 0o600))
			}
			_, body := e.get(t)
			testutil.Nil(t, body["context"])
			eqAny(t, body["live"], false)
		})
	}
}
