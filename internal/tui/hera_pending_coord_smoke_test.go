package tui

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
)

// pendingCoordFixture wires an App on the Hera tab whose coordinator create is
// held open on `release` and then returns (res, err) from `finish`.
type pendingCoordFixture struct {
	app     *App
	otherID int64
	d       *db.DB
	release chan struct{}
}

func newPendingCoordFixture(t *testing.T, finish func(d *db.DB) (*agent.HeraCoordinatorSpawnResult, error)) *pendingCoordFixture {
	t.Helper()
	d := testDB(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: t.TempDir()}))
	// A pre-existing orchestrator so "navigate away" has somewhere to go.
	other := seedHeraOrch(t, d, "other")
	seedHeraBoundRole(t, d, other, "coord", db.HeraKindCoordinator, "tother")
	app := New(d, agent.NewRunner(nil), false)
	app.heraOps = hera.NewOps(d)
	f := &pendingCoordFixture{app: app, d: d, otherID: other, release: make(chan struct{})}
	app.spawnHeraCoordinatorFn = func(d *db.DB, _ agent.SessionProvider, _ agent.HeraCoordinatorSpawnInput) (*agent.HeraCoordinatorSpawnResult, error) {
		<-f.release
		return finish(d)
	}
	return f
}

// createRealCoordinator mimics what SpawnHeraCoordinator persists.
func createRealCoordinator(d *db.DB) (*agent.HeraCoordinatorSpawnResult, error) {
	o, err := d.CreateHeraOrchestrator("brand-new", "")
	if err != nil {
		return nil, err
	}
	task := &model.Task{ID: "tnew", Name: "coord-brand-new", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}
	if err := d.Add(task); err != nil {
		return nil, err
	}
	role, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"})
	if err != nil {
		return nil, err
	}
	b, err := d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: task.ID, WorktreePath: "/wt/tnew"})
	if err != nil {
		return nil, err
	}
	return &agent.HeraCoordinatorSpawnResult{Orchestrator: o, Task: task, Role: role, Binding: b}, nil
}

func (f *pendingCoordFixture) submit(t *testing.T) {
	t.Helper()
	readUI(t, f.app.tapp, func() {
		f.app.switchTab(2)
		f.app.heraDoNewCoordinator("brand-new", &model.Task{Prompt: "do it", Project: "p"})
	})
}

func (f *pendingCoordFixture) state(t *testing.T) (selName string, selPending bool, focus hera.Focus, bannerPending bool, rows int) {
	t.Helper()
	readUI(t, f.app.tapp, func() {
		sel := f.app.heraPage.Rail().Selection()
		if sel.Orch != nil {
			selName = sel.Orch.Name
		}
		selPending = sel.IsPending()
		focus = f.app.heraPage.Machine().State()
		bannerPending = f.app.heraPage.CoordPane().Pending()
		rows = f.app.heraPage.PendingCoordinators()
	})
	return
}

func waitPendingResolved(t *testing.T, f *pendingCoordFixture) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		readUI(t, f.app.tapp, func() { n = f.app.heraPage.PendingCoordinators() })
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending coordinator never resolved")
}

// On submit the pending row is in the rail, selected, the coordinator pane
// shows the LAUNCH banner and holds focus — all before the create finishes.
// On completion focus is already on the live coordinator.
func TestSmoke_HeraNewCoordinator_PendingRowThenFocusOnCompletion(t *testing.T) {
	f := newPendingCoordFixture(t, createRealCoordinator)
	_, stop := wireApp(t, f.app)
	defer stop()

	f.submit(t)
	name, pending, focus, banner, n := f.state(t)
	testutil.Equal(t, name, "brand-new")
	testutil.Equal(t, pending, true)
	testutil.Equal(t, focus, hera.FocusCoord)
	testutil.Equal(t, banner, true)
	testutil.Equal(t, n, 1)

	close(f.release)
	waitPendingResolved(t, f)
	syncUI(t, f.app.tapp)

	name, pending, focus, banner, _ = f.state(t)
	testutil.Equal(t, name, "brand-new")
	testutil.Equal(t, pending, false)
	testutil.Equal(t, focus, hera.FocusCoord)
	testutil.Equal(t, banner, false)
	readUI(t, f.app.tapp, func() {
		testutil.Equal(t, f.app.heraPage.Rail().Selection().CoordTaskID(), "tnew")
		for _, o := range f.app.heraPage.Rail().Model().Active {
			testutil.Equal(t, o.Pending, false)
		}
	})
}

// Navigating off the pending row while it loads must not be undone by
// completion: the real orchestrator appears, but selection/focus stay put.
func TestSmoke_HeraNewCoordinator_NavigateAwaySuppressesFocusYank(t *testing.T) {
	f := newPendingCoordFixture(t, createRealCoordinator)
	sim, stop := wireApp(t, f.app)
	defer stop()

	f.submit(t)
	readUI(t, f.app.tapp, func() {
		f.app.heraPage.Machine().ToRail()
		testutil.Equal(t, f.app.heraPage.Rail().SelectOrch(f.otherID), true)
	})
	sim.InjectKey(tcell.KeyRune, 'x', 0) // harmless key on the rail: no-op
	name, pending, focus, banner, _ := f.state(t)
	testutil.Equal(t, name, "other")
	testutil.Equal(t, pending, false)
	testutil.Equal(t, focus, hera.FocusRail)
	testutil.Equal(t, banner, false) // pending banner leaves with the selection

	close(f.release)
	waitPendingResolved(t, f)
	syncUI(t, f.app.tapp)

	name, _, focus, _, _ = f.state(t)
	testutil.Equal(t, name, "other") // not yanked to the new coordinator
	testutil.Equal(t, focus, hera.FocusRail)
	readUI(t, f.app.tapp, func() {
		names := []string{}
		for _, o := range f.app.heraPage.Rail().Model().Active {
			names = append(names, o.Name)
		}
		testutil.Equal(t, slices.Contains(names, "brand-new"), true)
	})
}

// A failed create removes the pending row and surfaces the error; no orphan
// rows remain in the rail model.
func TestSmoke_HeraNewCoordinator_FailureRemovesPendingRow(t *testing.T) {
	f := newPendingCoordFixture(t, func(*db.DB) (*agent.HeraCoordinatorSpawnResult, error) {
		return nil, errors.New("boom")
	})
	_, stop := wireApp(t, f.app)
	defer stop()

	f.submit(t)
	_, pending, _, _, n := f.state(t)
	testutil.Equal(t, pending, true)
	testutil.Equal(t, n, 1)

	close(f.release)
	waitPendingResolved(t, f)
	syncUI(t, f.app.tapp)

	readUI(t, f.app.tapp, func() {
		testutil.Contains(t, f.app.statusbar.Error(), "boom")
		testutil.Equal(t, f.app.heraPage.CoordPane().Pending(), false)
		for _, o := range f.app.heraPage.Rail().Model().Active {
			testutil.Equal(t, o.Pending, false)
		}
		testutil.Equal(t, f.app.heraPage.Rail().Selection().IsPending(), false)
	})
}

// Mutation keys on the pending row are consumed and write nothing.
func TestSmoke_HeraNewCoordinator_PendingRowInertToMutations(t *testing.T) {
	f := newPendingCoordFixture(t, createRealCoordinator)
	sim, stop := wireApp(t, f.app)
	defer stop()

	f.submit(t)
	readUI(t, f.app.tapp, func() { f.app.heraPage.Machine().ToRail() })
	before, _ := f.d.ListHeraOrchestrators(false)
	for _, r := range []rune{'a', 'P', 's', 'r', 'w', 'B'} {
		sim.InjectKey(tcell.KeyRune, r, 0)
	}
	syncUI(t, f.app.tapp)
	sim.InjectKey(tcell.KeyCtrlD, 0, 0)
	syncUI(t, f.app.tapp)

	after, _ := f.d.ListHeraOrchestrators(false)
	testutil.Equal(t, len(after), len(before))
	readUI(t, f.app.tapp, func() {
		testutil.Equal(t, f.app.mode != modeNewTask, true)
		testutil.Equal(t, f.app.heraPage.Rail().Selection().IsPending(), true)
	})
	close(f.release)
	waitPendingResolved(t, f)
}
