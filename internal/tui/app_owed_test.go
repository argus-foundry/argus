package tui

import (
	"sync/atomic"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/app/agentview"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/tui/store"
	"github.com/drn/argus/internal/tui/terminal"
	"github.com/gdamore/tcell/v2"
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
	app.refreshAutoRerender() // the tick refresh
	owed, reason = app.paneRerenderOwed(task.ID, sess, 140)
	testutil.False(t, owed)
	testutil.Equal(t, reason, "setting-on")
}

// countingStore counts Config() calls: autoRerenderOn runs from every pane Draw,
// and a real Config() is several SQLite queries.
type countingStore struct {
	store.Store
	configCalls atomic.Int64
}

func (c *countingStore) Config() config.Config {
	c.configCalls.Add(1)
	return c.Store.Config()
}

// resize-without-restart (review blocker): neither autoRerenderOn nor the pane's
// Draw path may touch db.Config(). The setting is read once at startup and on the
// tick (refreshAutoRerender); N pane draws at a steady width do zero Config()
// reads and zero owed evaluations.
func TestAutoRerenderOn_CacheOnlyNoConfigPerDraw(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	cs := &countingStore{Store: d}
	app := New(cs, agent.NewRunner(nil), false)
	testutil.False(t, app.autoRerenderOn()) // primes the cache (one read)
	cs.configCalls.Store(0)

	for range 500 {
		testutil.False(t, app.autoRerenderOn())
	}
	testutil.Equal(t, cs.configCalls.Load(), int64(0))

	// A live flip is invisible until the tick refresh, then applied (one read).
	testutil.NoError(t, d.SetConfigValue("ui.auto_rerender_on_resize", "true"))
	testutil.False(t, app.autoRerenderOn())
	app.refreshAutoRerender()
	testutil.True(t, app.autoRerenderOn())
	testutil.Equal(t, cs.configCalls.Load(), int64(1))
}

func TestPaneDrawPath_NoConfigNoEvaluationAtSteadyWidth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	cs := &countingStore{Store: d}
	app := New(cs, agent.NewRunner(nil), false)
	_, stop := wireApp(t, app) // loadOwedAnchor queues a redraw: needs a running loop
	defer stop()
	task := &model.Task{ID: "draw-t", Name: "n", Status: model.StatusInProgress, SessionID: "sid", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(task))
	sess := &fakeKickSession{pid: 3, alive: true, initCols: 20}
	app.loadOwedAnchor(task.ID, sess)

	evals := 0
	pane := terminal.NewTerminalPane()
	pane.SetOwedFunc(func(id string, s agentview.TerminalAdapter, cols int) (bool, string) {
		evals++
		return app.paneRerenderOwed(id, s, cols)
	}, app.rerenderKeyHint)
	pane.SetTaskID(task.ID)
	pane.SetSession(sess)
	screen := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(120, 30)
	pane.SetRect(0, 0, 120, 30)

	pane.Draw(screen) // bind event: the one evaluation (primes caches)
	testutil.True(t, pane.RerenderOwed())
	cs.configCalls.Store(0)
	evals = 0
	for range 500 {
		pane.Draw(screen)
	}
	testutil.Equal(t, cs.configCalls.Load(), int64(0))
	testutil.Equal(t, evals, 0)
}
