package api

import (
	"database/sql"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskContext_CodexMeasured(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 11000, 1000, 200000))

	code, body := e.get(t)
	testutil.Equal(t, code, 200)
	eqAny(t, body["task_id"], e.task.ID)
	eqAny(t, body["session_id"], "thr1")
	eqAny(t, body["provider"], "openai")
	eqAny(t, body["source"], "codex_rollout")
	eqAny(t, body["live"], false) // no running session in the default runner
	eqAny(t, body["measured_at"], "2026-11-07T10:00:01Z")
	c := body["context"].(map[string]any)
	eqAny(t, c["used_tokens"], float64(12000))
	eqAny(t, c["window_tokens"], float64(200000))
	eqAny(t, c["percent"], float64(6))
	cats := c["categories"].(map[string]any)
	testutil.Equal(t, len(cats), 7)
	for _, v := range cats {
		testutil.True(t, v == nil)
	}
}

func TestTaskContext_CodexLiveWhenSessionRuns(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 10, 0, 100))
	e.s.runner = &bridgeTestRunner{SessionRunner: agent.NewRunner(nil), id: e.task.ID, session: &bridgeTestSession{}}
	_, body := e.get(t)
	eqAny(t, body["live"], true)
}

func TestTaskContext_CodexNullCases(t *testing.T) {
	t.Run("no thread in index", func(t *testing.T) {
		e := newCodexCtxEnv(t, "")
		code, body := e.get(t)
		testutil.Equal(t, code, 200)
		eqAny(t, body["provider"], "openai")
		eqAny(t, body["live"], false)
		testutil.True(t, body["context"] == nil)
		testutil.True(t, body["measured_at"] == nil)
	})
	t.Run("missing index file", func(t *testing.T) {
		e := newCodexCtxEnv(t, "")
		testutil.NoError(t, os.Remove(e.stateD))
		code, body := e.get(t)
		testutil.Equal(t, code, 200)
		testutil.True(t, body["context"] == nil)
	})
	t.Run("rollout without usage yet", func(t *testing.T) {
		e := newCodexCtxEnv(t, "")
		e.addThread(t, "thr1", "/w/cx", 1, `{"timestamp":"2026-11-07T10:00:00Z","type":"session_meta","payload":{}}`+"\n")
		_, body := e.get(t)
		testutil.True(t, body["context"] == nil)
	})
	t.Run("rollout file gone", func(t *testing.T) {
		e := newCodexCtxEnv(t, "")
		p := e.addThread(t, "thr1", "/w/cx", 1, "x")
		testutil.NoError(t, os.Remove(p))
		code, body := e.get(t)
		testutil.Equal(t, code, 200)
		testutil.True(t, body["context"] == nil)
	})
	t.Run("recorded session replaced by another thread", func(t *testing.T) {
		e := newCodexCtxEnv(t, "thr-old")
		e.addThread(t, "thr-new", "/w/cx", 9, codexTokenLine("2026-11-07T10:00:01Z", 500, 0, 1000))
		_, body := e.get(t)
		testutil.True(t, body["context"] == nil)
		testutil.True(t, body["measured_at"] == nil)
	})
	t.Run("recorded session from another worktree", func(t *testing.T) {
		e := newCodexCtxEnv(t, "thr-x")
		e.addThread(t, "thr-x", "/w/other", 9, codexTokenLine("2026-11-07T10:00:01Z", 500, 0, 1000))
		_, body := e.get(t)
		testutil.True(t, body["context"] == nil)
	})
	t.Run("unreadable path is logged not served", func(t *testing.T) {
		e := newCodexCtxEnv(t, "")
		conn, err := sql.Open("sqlite", e.stateD)
		testutil.NoError(t, err)
		defer conn.Close() //nolint:errcheck // test handle
		_, err = conn.Exec(`INSERT INTO threads (id,cwd,rollout_path,created_at,updated_at) VALUES ('thr1','/w/cx','/etc/passwd',1,1)`)
		testutil.NoError(t, err)
		code, body := e.get(t)
		testutil.Equal(t, code, 200)
		testutil.True(t, body["context"] == nil)
	})
}

func TestTaskContext_CodexSessionPinned(t *testing.T) {
	e := newCodexCtxEnv(t, "thr-old")
	e.addThread(t, "thr-old", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 100, 0, 1000))
	e.addThread(t, "thr-sub", "/w/cx", 9, codexTokenLine("2026-11-07T10:00:09Z", 900, 0, 1000))
	_, body := e.get(t)
	eqAny(t, body["session_id"], "thr-old")
	eqAny(t, body["context"].(map[string]any)["used_tokens"], float64(100))
}

func TestTaskContext_CodexMeasuredZeroVsUnknownWindow(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 0, 0, 0))
	_, body := e.get(t)
	c := body["context"].(map[string]any)
	eqAny(t, c["used_tokens"], float64(0))
	testutil.True(t, c["window_tokens"] == nil)
	testutil.True(t, c["percent"] == nil)
}

func TestTaskContext_CodexAccountHome(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	acct := t.TempDir()
	e.addThread(t, "thr-default", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 100, 0, 1000))
	testutil.NoError(t, e.s.db.SetAccount("work", config.Account{CodexHome: acct}))
	e.task.Account = "work"
	testutil.NoError(t, e.s.db.Update(e.task))
	// The account's own empty index must not fall through to the default one.
	_, body := e.get(t)
	testutil.True(t, body["context"] == nil)
}

func TestTaskContext_CodexRequiresAuthAnd404(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	code, _ := getContext(t, e.s, "nope")
	testutil.Equal(t, code, 404)
	h := authMiddleware(e.s.token, e.s.db, e.s.push, e.s.routes(), "/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/"+e.task.ID+"/context", nil))
	testutil.Equal(t, w.Code, 401)
}

func TestTaskContext_CodexEventOnAdvance(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	p := e.addThread(t, "thr1", "/w/cx", 1, codexTokenLine("2026-11-07T10:00:01Z", 5000, 0, 100000))
	state := newIdleWatcherState()
	ids := []string{e.task.ID}

	e.s.emitReaderContextChanges(state, ids)
	e.s.emitReaderContextChanges(state, ids)
	evs := sink.ofType(model.EventTypeTaskContextChanged)
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].TaskID, e.task.ID)
	testutil.False(t, strings.Contains(string(evs[0].Payload), "5000"))
	testutil.Equal(t, string(evs[0].Payload), `{"measured_at":"2026-11-07T10:00:01Z","session_id":"thr1"}`)

	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	testutil.NoError(t, err)
	_, err = f.WriteString(codexTokenLine("2026-11-07T10:00:09Z", 6000, 0, 100000))
	testutil.NoError(t, err)
	testutil.NoError(t, f.Close())
	e.s.emitReaderContextChanges(state, ids)
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskContextChanged)), 2)

	e.s.emitReaderContextChanges(state, nil)
	testutil.Equal(t, len(state.contextAt), 0)
}

func TestTaskContext_NonReaderBackendUnchanged(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	testutil.Equal(t, len(contextReaderTasks(s, task)), 0)
}

func contextReaderTasks(s *Server, task *model.Task) []string {
	fam, _ := s.contextFamily(task)
	if fam.ContextReader == nil {
		return nil
	}
	return []string{task.ID}
}
