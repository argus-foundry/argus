package tui

import (
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

// resize-without-restart: the owed notice is derived from the session's
// authored width (cached per process) and the pane width, never an RPC per
// frame, and is suppressed when the automatic kick is on.
func TestPaneRerenderOwed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	_, stop := wireApp(t, app) // loadOwedAnchor queues a redraw: needs a running loop
	defer stop()
	task := &model.Task{ID: "owed-t", Name: "n", Status: model.StatusInProgress, SessionID: "sid", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(task))
	sess := &fakeKickSession{pid: 7, alive: true, initCols: 80}

	// First sight only schedules the anchor load: nothing owed yet, no RPC inline.
	owed, _ := app.paneRerenderOwed(task.ID, sess, 140)
	testutil.False(t, owed)
	app.loadOwedAnchor(task.ID, sess) // what the background goroutine does

	owed, _ = app.paneRerenderOwed(task.ID, sess, 140) // 60 cols wider
	testutil.True(t, owed)
	owed, reason := app.paneRerenderOwed(task.ID, sess, 85) // back inside the margin
	testutil.False(t, owed)
	testutil.Equal(t, reason, "width-returned")

	// A restarted process (new pid) has a fresh authored width: not owed until
	// its anchor loads; the clear reason says it was re-rendered.
	restarted := &fakeKickSession{pid: 8, alive: true, initCols: 140}
	owed, reason = app.paneRerenderOwed(task.ID, restarted, 140)
	testutil.False(t, owed)
	testutil.Equal(t, reason, "rerendered")
	app.loadOwedAnchor(task.ID, restarted)
	owed, _ = app.paneRerenderOwed(task.ID, restarted, 140)
	testutil.False(t, owed)

	// Dead or missing session: never owed.
	owed, reason = app.paneRerenderOwed(task.ID, &fakeKickSession{pid: 8, alive: false}, 40)
	testutil.False(t, owed)
	testutil.Equal(t, reason, "no-session")

	// A task that can't resume (no session id) can't be re-rendered: no notice.
	nores := &model.Task{ID: "owed-nores", Name: "x", Status: model.StatusInProgress, Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(nores))
	s2 := &fakeKickSession{pid: 9, alive: true, initCols: 80}
	app.loadOwedAnchor(nores.ID, s2)
	owed, _ = app.paneRerenderOwed(nores.ID, s2, 160)
	testutil.False(t, owed)

	// Setting on: the kick handles it, so no notice.
	testutil.NoError(t, d.SetConfigValue("ui.auto_rerender_on_resize", "true"))
	owed, reason = app.paneRerenderOwed(task.ID, sess, 140)
	testutil.False(t, owed)
	testutil.Equal(t, reason, "setting-on")
}
