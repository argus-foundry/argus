package tui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/store"
	"github.com/gdamore/tcell/v2"
)

// refusingStore fails Delete / SetArchived with a fixed error and otherwise
// delegates. It does NOT expose GraduationPending (a remote-mode store).
type refusingStore struct {
	store.Store
	err error
}

func (r refusingStore) Delete(string) error            { return r.err }
func (r refusingStore) SetArchived(string, bool) error { return r.err }

// recRunner records session stops for a task that always has a session.
type recRunner struct {
	*agent.Runner
	stops atomic.Int32
}

func (r *recRunner) HasSession(string) bool { return true }
func (r *recRunner) Stop(string) error      { r.stops.Add(1); return nil }

func remote409() error {
	return &apiclient.Error{Status: http.StatusConflict, Method: "DELETE", Path: "/api/tasks/grad", Message: db.ErrGraduationPending.Error()}
}

// A refused delete must stop BEFORE any destructive side effect: no session
// stop, no log/artifact/worktree removal; the notice shows for both the local
// sentinel and the remote 409.
func TestDeleteTask_RefusedDeleteStopsCleanup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantNotice bool
	}{
		{"local sentinel", fmt.Errorf("%w (task grad)", db.ErrGraduationPending), true},
		{"remote 409", remote409(), true},
		{"other error", fmt.Errorf("boom"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, d, task, _ := pendingGraduateFixture(t)
			rr := &recRunner{Runner: agent.NewRunner(nil)}
			app.runner = rr
			app.db = refusingStore{Store: d, err: tc.err}
			var removed atomic.Int32
			orig := removeWorktreeAndBranch
			removeWorktreeAndBranch = func(string, string, string) { removed.Add(1) }
			defer func() { removeWorktreeAndBranch = orig }()
			logPath := agent.SessionLogPath(task.ID)
			testutil.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
			testutil.NoError(t, os.WriteFile(logPath, []byte("log"), 0o644))
			_, stop := wireApp(t, app)
			defer stop()

			readUI(t, app.tapp, func() { app.deleteTask(task) })
			time.Sleep(100 * time.Millisecond) // a wrongly launched cleanup goroutine would run by now
			testutil.Equal(t, rr.stops.Load(), int32(0))
			testutil.Equal(t, removed.Load(), int32(0))
			_, serr := os.Stat(logPath)
			testutil.NoError(t, serr)
			_, werr := os.Stat(task.Worktree)
			testutil.NoError(t, werr)
			if tc.wantNotice {
				testutil.Contains(t, app.header.Notice(), "graduation restart pending")
			} else {
				testutil.Equal(t, app.header.Notice(), "")
				testutil.Contains(t, app.statusbar.Error(), "Delete failed: boom")
			}
		})
	}

	t.Run("successful delete still cleans up", func(t *testing.T) {
		app, d, task, _ := pendingGraduateFixture(t)
		testutil.NoError(t, d.AbandonGraduationPending(task.ID))
		rr := &recRunner{Runner: agent.NewRunner(nil)}
		app.runner = rr
		var removed atomic.Int32
		orig := removeWorktreeAndBranch
		removeWorktreeAndBranch = func(string, string, string) { removed.Add(1) }
		defer func() { removeWorktreeAndBranch = orig }()
		_, stop := wireApp(t, app)
		defer stop()
		readUI(t, app.tapp, func() { app.deleteTask(task) })
		waitRerender(t, func() bool { return removed.Load() == 1 })
		testutil.Equal(t, rr.stops.Load(), int32(1))
	})
}

// Remote archive: the apiclient 409 never matches the sentinel with errors.Is,
// so the TUI must still show the notice.
func TestArchive_RemoteConflictShowsNotice(t *testing.T) {
	app, d, task, _ := pendingGraduateFixture(t)
	app.db = refusingStore{Store: d, err: remote409()}
	_, stop := wireApp(t, app)
	defer stop()
	toggled := *task
	toggled.Archived = true
	readUI(t, app.tapp, func() { app.tasklist.OnArchive(&toggled) })
	testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	readUI(t, app.tapp, func() { testutil.True(t, app.heraConfirmModal == nil) }) // remote offers no escape
}

// resumableGraduate makes the graduate's task resumable with a drifted
// session so an unguarded size-drift kick would stop it.
func resumableGraduate(t *testing.T, app *App, d *db.DB, task *model.Task) *fakeKickSession {
	t.Helper()
	testutil.NoError(t, d.SetConfigValue("ui.auto_rerender_on_resize", "true"))
	task.SessionID = "sid"
	testutil.NoError(t, d.Update(task))
	seedTUITranscript(t, os.Getenv("HOME"), task.Worktree, task.SessionID, time.Time{})
	sess := &fakeKickSession{idle: true, alive: true, initCols: 20, pid: 7}
	app.runner = getRunner{Runner: agent.NewRunner(nil), sess: sess}
	return sess
}

// The Tasks-tab agent view's automatic kick and forced hotkey must not
// kill+resume a pending graduate (the daemon owns that restart).
func TestPendingGraduate_TasksTabRerenderGuards(t *testing.T) {
	t.Run("auto kick", func(t *testing.T) {
		app, d, task, _ := pendingGraduateFixture(t)
		sess := resumableGraduate(t, app, d, task)
		_, stop := wireApp(t, app)
		defer stop()
		readUI(t, app.tapp, func() { app.maybeKickRerender(task, sess) })
		time.Sleep(150 * time.Millisecond)
		testutil.False(t, sess.stopCalled.Load())
		testutil.Equal(t, app.header.Notice(), "") // automatic path is silent

		// Control: once abandoned the very same call does kick.
		testutil.NoError(t, d.AbandonGraduationPending(task.ID))
		readUI(t, app.tapp, func() { app.maybeKickRerender(task, sess) })
		waitRerender(t, sess.stopCalled.Load)
	})
	t.Run("hotkey", func(t *testing.T) {
		app, d, task, _ := pendingGraduateFixture(t)
		sess := resumableGraduate(t, app, d, task)
		_, stop := wireApp(t, app)
		defer stop()
		app.mu.Lock()
		app.agentState.TaskID = task.ID
		app.mu.Unlock()
		readUI(t, app.tapp, func() { app.rerenderHotkeyClassic() })
		time.Sleep(150 * time.Millisecond)
		testutil.False(t, sess.stopCalled.Load())
		testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	})
}

// On the Projects tab the app-level guards are the only protection for the
// graduate's new coordinator pane.
func TestPendingGraduate_HeraRerenderGuards(t *testing.T) {
	t.Run("auto kick", func(t *testing.T) {
		app, d, task, _ := pendingGraduateFixture(t)
		sess := resumableGraduate(t, app, d, task)
		_, stop := wireApp(t, app)
		defer stop()
		readUI(t, app.tapp, func() { app.heraKickRerender(task.ID, 88, nil) })
		time.Sleep(150 * time.Millisecond)
		testutil.False(t, sess.stopCalled.Load())
		testutil.Equal(t, app.header.Notice(), "")

		testutil.NoError(t, d.AbandonGraduationPending(task.ID))
		readUI(t, app.tapp, func() { app.heraKickRerender(task.ID, 88, nil) })
		waitRerender(t, sess.stopCalled.Load)
	})
	t.Run("hotkey", func(t *testing.T) {
		app, d, task, _ := pendingGraduateFixture(t)
		sess := resumableGraduate(t, app, d, task)
		_, stop := wireApp(t, app)
		defer stop()
		readUI(t, app.tapp, func() { app.rerenderHotkeyHera(task.ID) })
		time.Sleep(150 * time.Millisecond)
		testutil.False(t, sess.stopCalled.Load())
		testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	})
}

// The confirm-delete modal must be closed BEFORE deleteTask so the abandon
// escape's own confirm survives.
func TestPendingGraduate_ConfirmDeleteKeyOpensEscape(t *testing.T) {
	app, _, task, _ := pendingGraduateFixture(t)
	_, stop := wireApp(t, app)
	defer stop()
	readUI(t, app.tapp, func() {
		app.openConfirmDelete(task)
		testutil.Equal(t, app.mode, modeConfirmDelete)
		app.handleConfirmDeleteKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
		testutil.Equal(t, app.mode, modeHeraConfirm)
		name, _ := app.pages.GetFrontPage()
		testutil.Equal(t, name, "heraconfirm")
		testutil.True(t, app.heraConfirmModal != nil)
		testutil.True(t, app.confirmDeleteModal == nil)
	})
}

// Adopting a pending graduate's coordinator is refused with the notice and
// leaves the orchestrator tree untouched.
func TestPendingGraduate_AdoptCoordinatorRefused(t *testing.T) {
	app, d, task, role := pendingGraduateFixture(t)
	app.heraAdoptOps = hera.NewAdoptOps(d)
	_, stop := wireApp(t, app)
	defer stop()
	_ = seedHeraOrch(t, d, "adopt-target")
	parentBefore, roleBefore, errBefore := d.HeraOrchLink(role.OrchestratorID)
	mode := app.mode
	readUI(t, app.tapp, func() {
		app.heraAdoptCoordinator(role.OrchestratorID, "grad", task.ID)
		testutil.Equal(t, app.mode, mode)
		testutil.Contains(t, app.header.Notice(), "graduation restart pending")
	})
	parentAfter, roleAfter, errAfter := d.HeraOrchLink(role.OrchestratorID)
	testutil.Equal(t, parentAfter, parentBefore)
	testutil.Equal(t, roleAfter, roleBefore)
	testutil.Equal(t, errAfter == nil, errBefore == nil)
}
