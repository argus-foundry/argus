package hera

import (
	"errors"
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

// seedSubCoordinator builds the dual-bound shape of an accepted-able
// sub-coordinator: ONE task holding a worker hat in the parent orchestrator
// and a coordinator hat in its own child orchestrator, plus the parent's
// coordinator (the accepter).
func seedSubCoordinator(t *testing.T, d *db.DB) (task *model.Task, workerRole, childCoord, parentCoord *db.HeraRole) {
	t.Helper()
	task, _, workerRole = seedRecycleRole(t, d, db.HeraKindWorker, "parent", "sub", "/wt/sub", "argus/sub", "mission")
	_, _, parentCoord = seedRecycleRole(t, d, db.HeraKindCoordinator, "parent", "coord", "/wt/coord", "argus/coord", "coordinate")
	child, err := d.CreateHeraOrchestrator("child", "master")
	testutil.NoError(t, err)
	childCoord, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: child.ID, Name: "sub-coord", Kind: db.HeraKindCoordinator, ArgusProject: task.Project,
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return task, workerRole, childCoord, parentCoord
}

func liveKinds(t *testing.T, d *db.DB, taskID string) map[db.HeraRoleKind]int {
	t.Helper()
	bs, err := d.ListHeraLiveBindingsByTask(taskID)
	testutil.NoError(t, err)
	out := map[db.HeraRoleKind]int{}
	for _, b := range bs {
		r, err := d.HeraRole(b.RoleID)
		testutil.NoError(t, err)
		out[r.Kind]++
	}
	return out
}

func TestAcceptRole_SubCoordinatorEndsCoordinatorBindingKeepsWorkerHat(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, worker, _, parent := seedSubCoordinator(t, d)
	testutil.Equal(t, liveKinds(t, d, task.ID)[db.HeraKindCoordinator], 1)

	flipped, err := AcceptRole(d, &fakeAcceptSender{}, parent.ID, worker.ID, "")
	testutil.NoError(t, err)
	testutil.True(t, flipped)

	k := liveKinds(t, d, task.ID)
	testutil.Equal(t, k[db.HeraKindCoordinator], 0) // the reaper guard no longer pins it
	testutil.Equal(t, k[db.HeraKindWorker], 1)      // still messageable and revivable
	g, err := d.LiveHeraKeepAliveBinding(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, g == nil)
}

func TestAcceptRole_PlainWorkerHasNoCoordinatorBindingToEnd(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, _, worker := seedRecycleRole(t, d, db.HeraKindWorker, "orch", "w", "/wt/w", "argus/w", "m")
	_, _, coord := seedRecycleRole(t, d, db.HeraKindCoordinator, "orch", "coord", "/wt/c", "argus/c", "c")
	_, err = AcceptRole(d, &fakeAcceptSender{}, coord.ID, worker.ID, "")
	testutil.NoError(t, err)
	testutil.Equal(t, liveKinds(t, d, task.ID)[db.HeraKindWorker], 1)
}

type endFailStore struct{ *db.DB }

func (endFailStore) EndHeraCoordinatorBindingsForTask(string, string) (int, error) {
	return 0, errors.New("db locked")
}

func TestAcceptRole_EndFailureIsReportedButStillCompletesAndNotifies(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, worker, _, parent := seedSubCoordinator(t, d)
	sender := &fakeAcceptSender{}

	flipped, err := AcceptRole(endFailStore{d}, sender, parent.ID, worker.ID, "")
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "end coordinator binding")
	testutil.True(t, flipped)
	testutil.Equal(t, len(sender.calls), 1)
	got, gerr := d.Get(task.ID)
	testutil.NoError(t, gerr)
	testutil.Equal(t, got.Status, model.StatusComplete)
}

func TestReviveRole_RestoresAcceptedSubCoordinatorBinding(t *testing.T) {
	for name, alive := range map[string]bool{"dead session restarted": false, "live idle session kicked": true} {
		t.Run(name, func(t *testing.T) {
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			task, worker, _, parent := seedSubCoordinator(t, d)
			task.SessionID = "sid"
			testutil.NoError(t, d.Update(task))
			_, err = AcceptRole(d, &fakeAcceptSender{}, parent.ID, worker.ID, "")
			testutil.NoError(t, err)
			testutil.Equal(t, liveKinds(t, d, task.ID)[db.HeraKindCoordinator], 0)

			// The accepted sub-coordinator's worker hat is the revive target
			// (isCoordinator=false), dead or live-idle.
			runner := &fakeReviveRunner{alive: alive, idle: true}
			_, err = ReviveRole(d, runner, task.ID, false)
			testutil.NoError(t, err)

			testutil.Equal(t, liveKinds(t, d, task.ID)[db.HeraKindCoordinator], 1) // working coordinator again
			g, gerr := d.LiveHeraKeepAliveBinding(task.ID)
			testutil.NoError(t, gerr)
			testutil.Equal(t, g.RoleName, "sub-coord")
		})
	}
}

func TestReviveDeadRole_RestoresAcceptedSubCoordinatorBinding(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, worker, _, parent := seedSubCoordinator(t, d)
	_, err = AcceptRole(d, &fakeAcceptSender{}, parent.ID, worker.ID, "")
	testutil.NoError(t, err)

	_, err = ReviveDeadRole(d, &fakeReviveRunner{alive: false}, task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, liveKinds(t, d, task.ID)[db.HeraKindCoordinator], 1)
}

type restoreFailStore struct{ *db.DB }

func (restoreFailStore) RestoreAcceptedHeraCoordinatorBindings(string) (int, error) {
	return 0, errors.New("db locked")
}

func TestReviveRole_RestoreFailureIsSaidClearly(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task, _, _, _ := seedSubCoordinator(t, d)

	outcome, err := ReviveRole(restoreFailStore{d}, &fakeReviveRunner{alive: false}, task.ID, false)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "coordinator binding could not be restored")
	testutil.Equal(t, outcome, ReviveRestartedDead) // the restart itself did happen
}
