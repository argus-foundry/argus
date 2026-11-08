package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type codexCtxEnv struct {
	s      *Server
	task   *model.Task
	dir    string
	stateD string
}

func codexTokenLine(ts string, in, out, window int64) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":900000,"output_tokens":900000,"total_tokens":1800000},"last_token_usage":{"input_tokens":%d,"output_tokens":%d,"total_tokens":%d},"model_context_window":%d}}}`+"\n",
		ts, in, out, in+out, window)
}

func newCodexCtxEnv(t *testing.T, sessionID string) *codexCtxEnv {
	t.Helper()
	s, d := testServer(t)
	dir := t.TempDir()
	t.Setenv("CODEX_SQLITE_HOME", dir)
	t.Setenv("CODEX_HOME", dir)
	testutil.NoError(t, os.Mkdir(filepath.Join(dir, "sessions"), 0o700))
	testutil.NoError(t, d.SetBackend("codex-test", config.Backend{Command: "codex"}))
	task := &model.Task{Name: "cx", Backend: "codex-test", Worktree: "/w/cx", SessionID: sessionID, Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	stateDB := filepath.Join(dir, "state_5.sqlite")
	conn, err := sql.Open("sqlite", stateDB)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`CREATE TABLE threads (id TEXT, cwd TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, source TEXT NOT NULL DEFAULT 'cli')`)
	testutil.NoError(t, err)
	return &codexCtxEnv{s: s, task: task, dir: dir, stateD: stateDB}
}

func (e *codexCtxEnv) addThread(t *testing.T, id, cwd string, updated int, body string) string {
	t.Helper()
	p := filepath.Join(e.dir, "sessions", "rollout-"+id+".jsonl")
	testutil.NoError(t, os.WriteFile(p, []byte(codexSessionMeta(id, cwd, "2026-11-07T10:00:00Z")+body), 0o600))
	conn, err := sql.Open("sqlite", e.stateD)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`INSERT INTO threads (id,cwd,rollout_path,created_at,updated_at) VALUES (?,?,?,?,?)`, id, cwd, p, updated, updated)
	testutil.NoError(t, err)
	return p
}

func (e *codexCtxEnv) get(t *testing.T) (int, map[string]any) {
	t.Helper()
	return getContext(t, e.s, e.task.ID)
}
