package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/app/agentview"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type unhealthyRunner struct{ agent.SessionProvider }

func (unhealthyRunner) SessionListHealthy() bool { return false }

func TestSessionListHealthy(t *testing.T) {
	testutil.Equal(t, sessionListHealthy(agent.NewRunner(nil)), true) // no signal → healthy
	testutil.Equal(t, sessionListHealthy(unhealthyRunner{}), false)
}

func TestApp_UpdateDisconnected(t *testing.T) {
	app, _ := newAppForStreamTest(t)
	app.tasks = []*model.Task{
		{ID: "dead", Status: model.StatusInProgress},
		{ID: "alive", Status: model.StatusInProgress},
		{ID: "done", Status: model.StatusComplete},
		{ID: "arch", Status: model.StatusInProgress, Archived: true},
		{ID: "review", Status: model.StatusInReview},
	}
	running := []string{"alive"}

	app.updateDisconnected(running, true) // tick 1: inside grace
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)

	app.updateDisconnected(running, false) // unreadable list: no new marks, counters kept
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)

	app.updateDisconnected(running, true) // tick 2: past grace
	testutil.DeepEqual(t, app.DisconnectedTaskIDs(), []string{"dead", "review"})

	app.updateDisconnected([]string{"alive", "dead", "review"}, true) // agents came back
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)
}

// show-disconnected-agents D4: Enter on a dead HANDLE of a still-alive agent
// reattaches the live session — no startSession, so no "already exists" error
// and no Pending flip.
func TestHandleAgentKey_EnterReattachesLiveAgentWithoutRestart(t *testing.T) {
	d := testDB(t)
	live := &fakeKickSession{pid: 7, alive: true}
	app := New(d, getRunner{Runner: agent.NewRunner(nil), sess: live}, false)

	testutil.NoError(t, d.Add(&model.Task{ID: "t1", Name: "t1", Status: model.StatusInProgress}))
	app.mode = modeAgent
	app.agentState.Reset("t1", "t1")
	app.agentPane.SetTaskID("t1")
	app.agentPane.SetSession(&fakeKickSession{alive: false}) // dead handle

	app.handleAgentKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	testutil.Equal(t, app.agentPane.Session() == agentview.TerminalAdapter(live), true)
	testutil.Equal(t, app.statusbar.Info(), "Reconnected")
	got, err := d.Get("t1")
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)
}

// A truly dead agent (runner has no session) still takes the existing
// startSession path on Enter.
// Item 2 (review): a pane with NO session but a live daemon session late-binds;
// that must also return before startSession.
func TestHandleAgentKey_EnterLateBindDoesNotRestart(t *testing.T) {
	d := testDB(t)
	live := &fakeKickSession{pid: 7, alive: true}
	app := New(d, getRunner{Runner: agent.NewRunner(nil), sess: live}, false)
	testutil.NoError(t, d.Add(&model.Task{ID: "t3", Name: "t3", Status: model.StatusInProgress}))
	app.mode = modeAgent
	app.agentState.Reset("t3", "t3")
	app.agentPane.SetTaskID("t3") // no session bound

	app.handleAgentKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	testutil.Equal(t, app.agentPane.Session() == agentview.TerminalAdapter(live), true)
	testutil.Equal(t, app.statusbar.Info(), "Reconnected")
	got, err := d.Get("t3")
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)
}

// Item 3 (review): a reattach restarts the PTY sync loop (new generation) so
// the reattached agent is resized; the old loop died with the old handle.
func TestReattachAgentPane_RestartsPTYSyncLoop(t *testing.T) {
	live := &fakeKickSession{pid: 9, alive: true}
	app := New(testDB(t), getRunner{Runner: agent.NewRunner(nil), sess: live}, false)
	app.agentPane.SetTaskID("t")
	app.agentPane.SetSession(&fakeKickSession{alive: false})
	before := app.ptySyncLoopGen["t"]

	testutil.Equal(t, app.reattachAgentPane("t"), true)
	testutil.Equal(t, app.ptySyncLoopGen["t"], before+1)

	// Nothing to attach (runner has no session): no loop, reports false.
	app2 := New(testDB(t), agent.NewRunner(nil), false)
	app2.agentPane.SetTaskID("x")
	testutil.Equal(t, app2.reattachAgentPane("x"), false)
	testutil.Equal(t, app2.ptySyncLoopGen["x"], uint64(0))
}

// Item 1 (review): a nil running list (RPC/HTTP failure, stale tick) is
// "unknown" — it must never mark tasks disconnected, however many ticks.
func TestApp_UpdateDisconnected_NilRunningFailsOpen(t *testing.T) {
	app, _ := newAppForStreamTest(t)
	app.tasks = []*model.Task{{ID: "a", Status: model.StatusInProgress}}
	for i := 0; i < 5; i++ {
		app.updateDisconnected(nil, true) // "healthy" provider that returned nil
	}
	testutil.Equal(t, len(app.DisconnectedTaskIDs()), 0)
	app.updateDisconnected([]string{}, true) // genuine empty list counts
	app.updateDisconnected([]string{}, true)
	testutil.DeepEqual(t, app.DisconnectedTaskIDs(), []string{"a"})
}

func TestHandleAgentKey_EnterOnGoneAgentStillStarts(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	testutil.NoError(t, d.Add(&model.Task{ID: "t2", Name: "t2", Status: model.StatusInProgress}))
	app.mode = modeAgent
	app.agentState.Reset("t2", "t2")
	app.agentPane.SetTaskID("t2")
	app.handleAgentKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	testutil.NotEqual(t, app.statusbar.Info(), "Reconnected")
	// startSession really ran: with no worktree it fails and reverts the task
	// to Pending (the same observable the closed-out revive test relies on), so
	// the genuinely-gone path is distinguishable from the reattach path, which
	// leaves the status untouched.
	got, err := d.Get("t2")
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusPending)
}

func TestApp_ReresolveAgentPane(t *testing.T) {
	live := &fakeKickSession{pid: 9, alive: true}
	app := New(testDB(t), getRunner{Runner: agent.NewRunner(nil), sess: live}, false)
	app.agentPane.SetTaskID("t")
	dead := &fakeKickSession{alive: false}
	app.agentPane.SetSession(dead)

	app.reresolveAgentPane("t", nil) // daemon does not report it alive → no RPC, no swap
	testutil.Equal(t, app.agentPane.Session() == agentview.TerminalAdapter(dead), true)

	app.reresolveAgentPane("t", []string{"t"}) // daemon says alive → fresh handle
	testutil.Equal(t, app.agentPane.Session() == agentview.TerminalAdapter(live), true)
}
