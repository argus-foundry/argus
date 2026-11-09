package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestEnsureResidentOrchestratorCollisionAndRollback(t *testing.T) {
	for _, tc := range []string{"collision", "binding failure"} {
		t.Run(tc, func(t *testing.T) {
			d := testDB(t)
			task, _, err := d.EnsureResidentTask(t.TempDir())
			testutil.NoError(t, err)
			if tc == "collision" {
				_, err = d.conn.Exec(`INSERT INTO hera_orchestrators(name,created_at) VALUES ('resident','2026-11-09')`)
			} else {
				_, err = d.conn.Exec(`CREATE TRIGGER fail_resident_binding BEFORE INSERT ON hera_bindings BEGIN SELECT RAISE(FAIL,'injected failure'); END`)
			}
			testutil.NoError(t, err)
			_, err = d.EnsureResidentOrchestrator(task.ID)
			testutil.True(t, err != nil)
			var n int
			testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_roles`).Scan(&n))
			testutil.Equal(t, n, 0)
			if tc == "binding failure" {
				testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_orchestrators`).Scan(&n))
				testutil.Equal(t, n, 0)
			} else {
				legacy, err := d.HeraOrchestratorByName("resident")
				testutil.NoError(t, err)
				visible, err := d.ListHeraOrchestrators(false)
				testutil.NoError(t, err)
				testutil.Equal(t, len(visible), 1)
				yes, err := d.IsResidentOrchestrator(legacy.ID)
				testutil.NoError(t, err)
				testutil.False(t, yes)
				testutil.NoError(t, d.RenameHeraOrchestrator(legacy.ID, "legacy"))
				_, err = d.EnsureResidentOrchestrator(task.ID)
				testutil.NoError(t, err)
			}
		})
	}
}

func TestResidentWorkerMembershipAndCount(t *testing.T) {
	d := testDB(t)
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	_, b, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: HeraKindWorker}, "worker", t.TempDir())
	testutil.NoError(t, err)
	n, err := d.ResidentWorkerCount()
	testutil.NoError(t, err)
	testutil.Equal(t, n, 1)
	yes, err := d.IsResidentWorker("worker")
	testutil.NoError(t, err)
	testutil.True(t, yes)
	testutil.NoError(t, d.EndHeraBinding(b.ID, "graduated"))
	n, err = d.ResidentWorkerCount()
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)
	yes, err = d.IsResidentWorker("worker")
	testutil.NoError(t, err)
	testutil.False(t, yes)
	testutil.NoError(t, d.SetMeta("worker", "hera", "resident_worker", "true"))
	yes, err = d.IsResidentWorker("worker")
	testutil.NoError(t, err)
	testutil.True(t, yes)
}

func TestEnsureResidentOrchestrator(t *testing.T) {
	d := testDB(t)
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	again, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, again.ID, o.ID)
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, b.OrchestratorID, o.ID)
	visible, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(visible), 0)
	_, err = d.CreateHeraOrchestrator("resident", "")
	testutil.ErrorIs(t, err, ErrResidentProtected)
	testutil.ErrorIs(t, d.NukeHeraOrchestrator(o.ID), ErrResidentProtected)
	testutil.ErrorIs(t, d.ArchiveHeraRole(b.RoleID), ErrResidentProtected)
	testutil.ErrorIs(t, d.EndHeraBinding(b.ID, "test"), ErrResidentProtected)
}
