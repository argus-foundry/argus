package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/testutil"
)

// ctrl+r session switch rewrites SessionID and stops the session: it must
// refuse for a pending graduate (same-conversation resume).
func TestPendingGraduate_SessionSwitchRefused(t *testing.T) {
	app, d, task, _ := pendingGraduateFixture(t)
	sess := resumableGraduate(t, app, d, task)
	_, stop := wireApp(t, app)
	defer stop()
	app.mu.Lock()
	app.agentState.TaskID = task.ID
	app.mu.Unlock()
	readUI(t, app.tapp, func() { app.switchSession("other-session", "t") })
	time.Sleep(100 * time.Millisecond)
	testutil.False(t, sess.stopCalled.Load())
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.SessionID, "sid")
	testutil.Contains(t, app.header.Notice(), "graduation restart pending")
}

// A forced hotkey re-render deferred before the graduation landed keeps its
// retry armed; the retry must drop it instead of kill+resuming alongside the
// daemon watcher.
func TestPendingGraduate_DeferredForcedRetryDropped(t *testing.T) {
	arm := func(app *App, id string) {
		app.mu.Lock()
		app.agentState.TaskID = id
		app.mu.Unlock()
		app.kickForced[id] = time.Now()
		app.kickWait[id] = time.Now().Add(-time.Minute)
		delete(app.kickRetryAfter, id)
	}
	app, d, task, _ := pendingGraduateFixture(t)
	sess := resumableGraduate(t, app, d, task)
	_, stop := wireApp(t, app)
	defer stop()
	readUI(t, app.tapp, func() { arm(app, task.ID); app.retryDeferredKick(task.ID) })
	time.Sleep(150 * time.Millisecond)
	testutil.False(t, sess.stopCalled.Load())
	readUI(t, app.tapp, func() {
		_, forced := app.kickForced[task.ID]
		_, waiting := app.kickWait[task.ID]
		testutil.False(t, forced)
		testutil.False(t, waiting)
	})

	// Control: after abandon the same armed retry kicks.
	testutil.NoError(t, d.AbandonGraduationPending(task.ID))
	readUI(t, app.tapp, func() { arm(app, task.ID); app.retryDeferredKick(task.ID) })
	waitRerender(t, sess.stopCalled.Load)
}

func TestPendingGraduate_AccountMoveRefused(t *testing.T) {
	app, d, task, _ := pendingGraduateFixture(t)
	sess := resumableGraduate(t, app, d, task)
	_, stop := wireApp(t, app)
	defer stop()
	readUI(t, app.tapp, func() { app.runAccountMove(task, "work", false) })
	time.Sleep(100 * time.Millisecond)
	testutil.False(t, sess.stopCalled.Load())
	testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Account, "")
}

// The TUI pre-guard refuses before any "Moving..." progress is shown; the
// store-level refusal (which would also flash the notice, but only after the
// info line was set) must not be what is being observed.
func TestPendingGraduate_AccountMovePreGuard(t *testing.T) {
	app, _, task, _ := pendingGraduateFixture(t)
	_, stop := wireApp(t, app)
	defer stop()
	readUI(t, app.tapp, func() {
		app.runAccountMove(task, "work", false)
		testutil.Equal(t, app.statusbar.Info(), "")
		testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	})
}

// Remote mode: the daemon's 409 arrives as an apiclient.Error that does not
// wrap the sentinel; it must surface as the pending notice, not a red error.
func TestAccountMoveFailed_RemoteGraduationConflict(t *testing.T) {
	app, _, task, _ := pendingGraduateFixture(t)
	_, stop := wireApp(t, app)
	defer stop()
	readUI(t, app.tapp, func() {
		app.accountMoveFailed(task, &apiclient.Error{Status: 409, Message: "graduation restart pending"})
		testutil.Contains(t, app.header.Notice(), "graduation restart pending")
		testutil.Equal(t, app.statusbar.Error(), "")
	})
}

// The ctrl+r switcher refuses to open for a pending graduate (it would
// rewrite the session id the daemon watcher resumes from).
func TestPendingGraduate_SessionPickerOpenRefused(t *testing.T) {
	app, d, task, _ := pendingGraduateFixture(t)
	_, stop := wireApp(t, app)
	defer stop()
	app.mu.Lock()
	app.mode = modeAgent
	app.agentState.TaskID = task.ID
	app.mu.Unlock()
	readUI(t, app.tapp, func() { app.openSessionPicker() })
	readUI(t, app.tapp, func() { testutil.True(t, app.sessionPickerModal == nil) })
	testutil.Contains(t, app.header.Notice(), "graduation restart pending")

	// Control: once abandoned the guard no longer refuses.
	app.header.ClearNotice()
	testutil.NoError(t, d.AbandonGraduationPending(task.ID))
	readUI(t, app.tapp, func() { app.openSessionPicker() })
	if n := app.header.Notice(); strings.Contains(n, "graduation restart pending") {
		t.Fatalf("guard still refusing after abandon: %q", n)
	}
}
