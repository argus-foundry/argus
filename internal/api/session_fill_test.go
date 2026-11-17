package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const fillThreadID = "11111111-2222-4333-8444-555555555555"

// codexFillServer is a server whose Codex task has an empty SessionID while
// Codex's own thread index already holds a thread for the task's worktree.
func codexFillServer(t *testing.T) (*Server, *moveTestRunner, *model.Task) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	sqliteHome := filepath.Join(home, "codex-state")
	t.Setenv("CODEX_SQLITE_HOME", sqliteHome)
	s, d := testServer(t)
	r := &moveTestRunner{SessionRunner: agent.NewRunner(nil)}
	s.runner = r
	testutil.NoError(t, d.SetBackend("cx", config.Backend{Command: "codex --dangerously-bypass-approvals-and-sandbox"}))
	wt := t.TempDir()
	task := &model.Task{Name: "codex", Backend: "cx", Worktree: wt, Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))

	testutil.NoError(t, os.MkdirAll(sqliteHome, 0o700))
	conn, err := sql.Open("sqlite", filepath.Join(sqliteHome, "state_5.sqlite"))
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Exec(`CREATE TABLE threads (id TEXT, cwd TEXT, updated_at INTEGER, source TEXT NOT NULL DEFAULT 'cli')`)
	testutil.NoError(t, err)
	_, err = conn.Exec(`INSERT INTO threads (id,cwd,updated_at) VALUES (?, ?, 1)`, fillThreadID, wt)
	testutil.NoError(t, err)
	return s, r, task
}

// The resume handler must refresh the session ID BEFORE deciding to resume, or
// a Codex task whose ID was never recorded relaunches with its first prompt.
func TestHandleResumeTask_FillsMissingCodexIDBeforeResume(t *testing.T) {
	s, r, task := codexFillServer(t)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/resume", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.True(t, r.startResume)
	testutil.Equal(t, r.started.SessionID, fillThreadID)
}

func TestStartTaskSessionFill_Switch(t *testing.T) {
	t.Run("fill enabled adopts the thread", func(t *testing.T) {
		s, r, task := codexFillServer(t)
		_, err := s.startTaskSessionFill(task, true, true)
		testutil.NoError(t, err)
		testutil.Equal(t, r.started.SessionID, fillThreadID)
	})
	t.Run("fill disabled keeps a deliberately empty id (backend move)", func(t *testing.T) {
		s, r, task := codexFillServer(t)
		_, err := s.startTaskSessionFill(task, true, false)
		testutil.NoError(t, err)
		testutil.Equal(t, r.started.SessionID, "")
	})
}
