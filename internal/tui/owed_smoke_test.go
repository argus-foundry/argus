package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

// owedAgentView opens the Tasks-tab agent view on a finished-looking task and
// swaps in a live fake session that was authored at initCols, so the real pane
// width (80x24 sim, zoomed) drifts past the margin.
func owedAgentView(t *testing.T, initCols int) (*App, tcell.SimulationScreen, *fakeKickSession, *model.Task) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	task := &model.Task{ID: "owed-smoke", Name: "owed", Status: model.StatusComplete, SessionID: "sid", Project: "p", Worktree: t.TempDir(), CreatedAt: time.Now()}
	testutil.NoError(t, d.Add(task))
	seedTUITranscript(t, os.Getenv("HOME"), task.Worktree, task.SessionID, time.Time{})
	app.refreshTasks()
	sim, stop := wireApp(t, app)
	t.Cleanup(stop)

	sess := &fakeKickSession{pid: 55, alive: true, idle: true, initCols: initCols}
	readUI(t, app.tapp, func() {
		app.runner = getRunner{Runner: agent.NewRunner(nil), sess: sess}
		app.onTaskSelect(task, false) // open the agent view on the live fake session
		testutil.Equal(t, app.mode, modeAgent)
	})
	syncUI(t, app.tapp)
	return app, sim, sess, task
}

func topBorderWith(t *testing.T, app *App, sim tcell.SimulationScreen, needle string) (row string, y int) {
	t.Helper()
	deadline := time.Now().Add(uiTimeout)
	for time.Now().Before(deadline) {
		syncUI(t, app.tapp)
		for yy := 0; yy < 24; yy++ {
			r := screenRow(sim, yy, 80)
			if strings.Contains(r, needle) && strings.Contains(r, "Agent") {
				return r, yy
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no frame row containing both the pane title and %q:\n%s", needle, screenText(sim, 80, 24))
	return "", 0
}

// Resize with the setting off: the notice appears on the pane frame's top
// border, on the same row as the pane title (title left, notice right), and
// nothing restarts.
func TestSmoke_OwedNoticeTopBorderRight_Tasks(t *testing.T) {
	app, sim, sess, _ := owedAgentView(t, 20)
	row, _ := topBorderWith(t, app, sim, "width changed · ctrl+n re-render")
	testutil.True(t, strings.Index(row, "Agent") < strings.Index(row, "width changed"))
	testutil.False(t, sess.stopCalled.Load())
}

// ctrl+n in the focused agent pane runs the re-render (intercepted, not
// forwarded) with the automatic kick off.
func TestSmoke_Ctrl_N_ReRendersTasksAgentView(t *testing.T) {
	app, sim, sess, _ := owedAgentView(t, 20)
	topBorderWith(t, app, sim, "width changed")
	sim.InjectKey(tcell.KeyCtrlN, 0, 0)
	syncUI(t, app.tapp)
	waitRerender(t, sess.stopCalled.Load)
}

// A click on the notice does the same, and a click elsewhere on the border or
// body does not.
func TestSmoke_ClickOwedNotice_Rerenders(t *testing.T) {
	app, sim, sess, _ := owedAgentView(t, 20)
	row, y := topBorderWith(t, app, sim, "width changed")
	// Cell x of the notice (the row is one string per cell, so byte index of a
	// multi-byte rune may differ; count runes up to the needle).
	x := len([]rune(row[:strings.Index(row, "width changed")])) + 3

	sim.InjectMouse(x, y+8, tcell.Button1, 0) // pane body: no re-render
	syncUI(t, app.tapp)
	sim.InjectMouse(x, y+8, tcell.ButtonNone, 0) // release, so the next press is a fresh click
	syncUI(t, app.tapp)
	testutil.False(t, sess.stopCalled.Load())

	sim.InjectMouse(x, y, tcell.Button1, 0)
	syncUI(t, app.tapp)
	sim.InjectMouse(x, y, tcell.ButtonNone, 0)
	syncUI(t, app.tapp)
	waitRerender(t, sess.stopCalled.Load)
}

// The notice goes away when the setting is turned on (the kick owns it) —
// live, no restart.
func TestSmoke_OwedNoticeClearsWhenSettingOn(t *testing.T) {
	app, sim, _, _ := owedAgentView(t, 20)
	topBorderWith(t, app, sim, "width changed")
	testutil.NoError(t, app.db.SetConfigValue("ui.auto_rerender_on_resize", "true"))
	deadline := time.Now().Add(uiTimeout)
	for time.Now().Before(deadline) {
		app.tapp.QueueUpdateDraw(func() { app.refreshAutoRerender() }) // the tick's refresh + repaint
		syncUI(t, app.tapp)
		if !strings.Contains(screenText(sim, 80, 24), "width changed") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("notice still showing after the setting turned on:\n%s", screenText(sim, 80, 24))
}

// A window resize and a Ctrl+Z zoom toggle pair with the setting off resize the
// PTY and nothing else: the session is never stopped and no kick is logged.
func TestSmoke_ResizeDoesNotRestart(t *testing.T) {
	app, sim, sess, task := owedAgentView(t, 80)
	topBorderWith(t, app, sim, "Agent")
	sim.SetSize(140, 30)
	sim.InjectKey(tcell.KeyCtrlZ, 0, 0)
	syncUI(t, app.tapp)
	sim.InjectKey(tcell.KeyCtrlZ, 0, 0)
	syncUI(t, app.tapp)
	app.tapp.QueueUpdateDraw(func() {})
	syncUI(t, app.tapp)
	time.Sleep(200 * time.Millisecond) // let any (wrongly) spawned decision goroutine act
	testutil.False(t, sess.stopCalled.Load())
	readUI(t, app.tapp, func() {
		testutil.False(t, app.pendingRerenderRestart[task.ID])
		testutil.Equal(t, sess.PID(), 55)
	})
}
