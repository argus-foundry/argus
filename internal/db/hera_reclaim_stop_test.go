package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPinMissingTaskDoesNotCreateReclaimLock(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.ErrorIs(t, d.SetPinned("missing", true), ErrTaskNotFound)
	_, err = os.Stat(d.sessionLockDir)
	testutil.ErrorIs(t, err, os.ErrNotExist)
}

func TestHeraReclaimStopPersistsForExactSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	tui, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = tui.Close() })
	daemon, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = daemon.Close() })

	task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress}
	testutil.NoError(t, tui.Add(task))
	armed, err := tui.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, armed)
	testutil.NoError(t, tui.SetArchived(task.ID, true))
	armed, err = tui.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	matched, err := daemon.ConsumeHeraReclaimStop(task.ID, 42)
	testutil.NoError(t, err)
	testutil.False(t, matched)
	testutil.NoError(t, tui.ClearHeraReclaimStop(task.ID, 42))
	matched, err = daemon.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, matched)
	matched, err = daemon.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)

	persisted, err := daemon.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, persisted.Status, model.StatusInProgress)
}

func TestHeraReclaimStopRespectsCurrentTaskState(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress, Archived: true}
	testutil.NoError(t, d.Add(task))

	_, err := d.ArmHeraReclaimStop(task.ID, 0)
	testutil.Error(t, err)
	matched, err := d.ConsumeHeraReclaimStop(task.ID, 0)
	testutil.NoError(t, err)
	testutil.False(t, matched)

	armed, err := d.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	testutil.ErrorIs(t, d.SetArchived(task.ID, false), ErrHeraReclaimInProgress)
	// A prior daemon could have allowed restoring an armed task.
	_, err = d.conn.Exec(`UPDATE tasks SET archived=0 WHERE id=?`, task.ID)
	testutil.NoError(t, err)
	matched, err = d.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)
	testutil.NoError(t, d.ArchiveHeraReclaim(task.ID))
	matched, err = d.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)
}

func TestHeraBindingAdmissionWaitsForReclaimExit(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress, Archived: true}
	testutil.NoError(t, d.Add(task))
	orch, err := d.CreateHeraOrchestrator("new-team", "")
	testutil.NoError(t, err)
	role := CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "joining", Kind: HeraKindCoordinator}
	armed, err := d.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	_, _, err = d.CreateHeraRoleWithBinding(role, task.ID, "")
	testutil.ErrorIs(t, err, ErrHeraReclaimInProgress)
	testutil.NoError(t, d.ClearHeraReclaimStop(task.ID, 41))
	_, _, err = d.CreateHeraRoleWithBinding(role, task.ID, "")
	testutil.NoError(t, err)
}

func TestNukedTaskReclaimOwnsLaunchAndPrune(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "nuked", Name: "Nuked", Status: model.StatusComplete}
	testutil.NoError(t, d.Add(task))
	orch, err := d.CreateHeraOrchestrator("reclaim", "")
	testutil.NoError(t, err)
	role, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: orch.ID, Name: "worker", Kind: HeraKindWorker,
	}, task.ID, "")
	testutil.NoError(t, err)
	testutil.NoError(t, d.NukeHeraRoleAndEndBinding(role.ID))
	testutil.ErrorIs(t, d.SetArchived(task.ID, true), ErrHeraReclaimInProgress)
	archived := *task
	archived.SetArchived(true)
	testutil.ErrorIs(t, d.Update(&archived), ErrHeraReclaimInProgress)
	testutil.NoError(t, d.ArchiveHeraReclaim(task.ID))

	checkGenericOperations := func() {
		t.Helper()
		unlock, acquired, err := d.TryLockSessionLaunch(task.ID)
		if acquired {
			unlock()
		}
		testutil.ErrorIs(t, err, ErrHeraReclaimInProgress)
		testutil.ErrorIs(t, d.Delete(task.ID), ErrHeraReclaimInProgress)
		pruned, skipped, err := d.PruneCompleted()
		testutil.NoError(t, err)
		testutil.Equal(t, len(pruned), 0)
		testutil.Equal(t, skipped, 1)
	}
	checkGenericOperations()
	armed, err := d.ArmHeraReclaimStop(task.ID, 42)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	checkGenericOperations()
	pruned, _, err := d.PruneHeraReclaimedTask(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(pruned), 0)
	testutil.NoError(t, d.ClearHeraReclaimStop(task.ID, 42))
	checkGenericOperations()
	pruned, _, err = d.PruneHeraReclaimedTask(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(pruned), 1)
}

func TestPreservedHeraNukeAllowsRemainingRoleRebind(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "shared", Name: "Shared", Status: model.StatusComplete}
	testutil.NoError(t, d.Add(task))
	first, err := d.CreateHeraOrchestrator("first", "")
	testutil.NoError(t, err)
	second, err := d.CreateHeraOrchestrator("second", "")
	testutil.NoError(t, err)
	role, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: first.ID, Name: "first-worker", Kind: HeraKindWorker,
	}, task.ID, "")
	testutil.NoError(t, err)
	remaining, binding, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: second.ID, Name: "second-worker", Kind: HeraKindWorker,
	}, task.ID, "")
	testutil.NoError(t, err)

	testutil.NoError(t, d.NukeHeraRoleAndEndBinding(role.ID))
	testutil.NoError(t, d.EndHeraBinding(binding.ID, "hera_rebind"))
	candidates, err := d.HeraNukeReclaimCandidates()
	testutil.NoError(t, err)
	testutil.Equal(t, len(candidates), 0)
	allowed, err := d.HasUnsettledHeraNuke(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, allowed)
	unlock, acquired, err := d.TryLockSessionLaunch(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	unlock()
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: remaining.ID, ArgusTaskID: task.ID})
	testutil.NoError(t, err)
}

func TestClearOlderHiddenHeraRoleAfterReclaimSettled(t *testing.T) {
	d := testDB(t)
	const id = "shared"
	testutil.NoError(t, d.Add(&model.Task{ID: id, Name: "Shared"}))
	first, err := d.CreateHeraOrchestrator("first", "")
	testutil.NoError(t, err)
	second, err := d.CreateHeraOrchestrator("second", "")
	testutil.NoError(t, err)
	older, binding, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: first.ID, Name: "older", Kind: HeraKindWorker,
	}, id, "")
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(binding.ID, "archived"))
	testutil.NoError(t, d.ArchiveHeraRole(older.ID))
	newer, _, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: second.ID, Name: "newer", Kind: HeraKindWorker,
	}, id, "")
	testutil.NoError(t, err)
	testutil.NoError(t, d.NukeHeraRoleAndEndBinding(newer.ID))
	testutil.NoError(t, d.ArchiveHeraReclaim(id))
	testutil.NoError(t, d.MarkHeraReclaimCleaned(id))
	testutil.NoError(t, d.SetArchived(id, false))

	testutil.NoError(t, d.NukeHeraRoleAndEndBinding(older.ID))
	testutil.ErrorIs(t, d.SetArchived(id, true), ErrHeraReclaimInProgress)
	candidates, err := d.HeraNukeReclaimCandidates()
	testutil.NoError(t, err)
	testutil.Equal(t, len(candidates), 1)
}

func TestLegacyHeraNukeUsesBindingHistory(t *testing.T) {
	for _, tc := range []struct {
		name, siblingEnd string
		reclaim          bool
	}{
		{"sibling outlived nuke", "2026-01-01T00:00:00.000100002Z", false},
		{"sibling ended later on UTC clock", "2026-01-01T02:00:00.000100Z", false},
		{"nuke followed prior detachment", "2026-01-01T02:00:00.000100+02:00", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDB(t)
			const id = "legacy"
			testutil.NoError(t, d.Add(&model.Task{ID: id, Name: "Legacy"}))
			first, err := d.CreateHeraOrchestrator("first", "")
			testutil.NoError(t, err)
			second, err := d.CreateHeraOrchestrator("second", "")
			testutil.NoError(t, err)
			role, nuke, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
				OrchestratorID: first.ID, Name: "first-worker", Kind: HeraKindWorker,
			}, id, "")
			testutil.NoError(t, err)
			remaining, sibling, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
				OrchestratorID: second.ID, Name: "second-worker", Kind: HeraKindWorker,
			}, id, "")
			testutil.NoError(t, err)
			// Old clients marked the role and ended its binding without recording reclaim ownership.
			testutil.NoError(t, d.NukeHeraRole(role.ID))
			testutil.NoError(t, d.EndHeraBinding(nuke.ID, HeraEndReasonUserDeleted))
			testutil.NoError(t, d.EndHeraBinding(sibling.ID, "hera_rebind"))
			_, err = d.conn.Exec(`UPDATE hera_bindings SET ended_at=CASE WHEN id=? THEN ? ELSE ? END WHERE id IN (?,?)`,
				nuke.ID, "2026-01-01T00:00:00.000100001Z", tc.siblingEnd, nuke.ID, sibling.ID)
			testutil.NoError(t, err)

			claimed, err := d.HasUnsettledHeraNuke(id)
			testutil.NoError(t, err)
			testutil.Equal(t, claimed, tc.reclaim)
			candidates, err := d.HeraNukeReclaimCandidates()
			testutil.NoError(t, err)
			testutil.Equal(t, len(candidates) == 1, tc.reclaim)
			if tc.reclaim {
				testutil.NoError(t, d.SetArchived(id, true))
			}
			_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: remaining.ID, ArgusTaskID: id})
			if tc.reclaim {
				testutil.ErrorIs(t, err, ErrHeraReclaimInProgress)
			} else {
				testutil.NoError(t, err)
			}
		})
	}
}

func TestHiddenHeraNukeRefusesPendingGraduation(t *testing.T) {
	d := testDB(t)
	const id = "graduate"
	testutil.NoError(t, d.Add(&model.Task{ID: id, Name: "Graduate"}))
	orch, err := d.CreateHeraOrchestrator("team", "")
	testutil.NoError(t, err)
	role, binding, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{
		OrchestratorID: orch.ID, Name: "worker", Kind: HeraKindWorker,
	}, id, "")
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(binding.ID, "archived"))
	testutil.NoError(t, d.ArchiveHeraRole(role.ID))
	testutil.NoError(t, d.SetMeta(id, HeraMetaNamespace, HeraMetaKeyGraduationPendingRestart, "true"))

	testutil.ErrorIs(t, d.NukeHeraRoleAndEndBinding(role.ID), ErrGraduationPending)
	got, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.Nil(t, got.NukedAt)
	claimed, err := d.HasUnsettledHeraNuke(id)
	testutil.NoError(t, err)
	testutil.False(t, claimed)
}

func TestReconcileMissingSessionPreservesArmedReclaim(t *testing.T) {
	d := testDB(t)
	const pid = 41
	task := &model.Task{ID: "reclaim", Name: "Reclaim", AgentPID: pid, Status: model.StatusInProgress, Archived: true}
	testutil.NoError(t, d.Add(task))
	armed, err := d.ArmHeraReclaimStop(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	changed, err := d.ReconcileMissingSession(task.ID, pid)
	testutil.NoError(t, err)
	testutil.False(t, changed)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)

	testutil.NoError(t, d.ClearHeraReclaimStop(task.ID, pid))
	changed, err = d.ReconcileMissingSession(task.ID, pid-1)
	testutil.NoError(t, err)
	testutil.False(t, changed)
	changed, err = d.ReconcileMissingSession(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, changed)
	got, err = d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInReview)
}
