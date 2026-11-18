package tui

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// refreshSessionIDBeforeStart must fill a missing Codex ID for an ordinary
// start but leave it empty for a backend handoff, which clears the ID on
// purpose so the new backend starts from a seed prompt.
func TestRefreshSessionIDBeforeStart_FillSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	sqliteHome := filepath.Join(home, "codex-state")
	t.Setenv("CODEX_SQLITE_HOME", sqliteHome)
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)

	wt := t.TempDir()
	const threadID = "11111111-2222-4333-8444-555555555555"
	testutil.NoError(t, os.MkdirAll(sqliteHome, 0o700))
	conn, err := sql.Open("sqlite", filepath.Join(sqliteHome, "state_5.sqlite"))
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Exec(`CREATE TABLE threads (id TEXT, cwd TEXT, updated_at INTEGER, source TEXT NOT NULL DEFAULT 'cli')`)
	testutil.NoError(t, err)
	_, err = conn.Exec(`INSERT INTO threads (id,cwd,updated_at) VALUES (?, ?, 1)`, threadID, wt)
	testutil.NoError(t, err)

	t.Run("ordinary start fills", func(t *testing.T) {
		task := &model.Task{Name: "a", Project: "p", Backend: "codex", Worktree: wt}
		testutil.NoError(t, d.Add(task))
		app.refreshSessionIDBeforeStart(task, agent.RefreshFillMissing)
		testutil.Equal(t, task.SessionID, threadID)
	})
	t.Run("backend handoff does not fill", func(t *testing.T) {
		task := &model.Task{Name: "b", Project: "p", Backend: "codex", Worktree: wt}
		testutil.NoError(t, d.Add(task))
		app.refreshSessionIDBeforeStart(task, agent.RefreshKeepEmpty)
		testutil.Equal(t, task.SessionID, "")
	})
}
