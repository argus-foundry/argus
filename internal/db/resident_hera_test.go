package db

import (
	"time"

	"testing"

	"github.com/argus-foundry/argus/internal/model"

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
	_, b, err := d.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: HeraKindWorker}, "worker", t.TempDir())
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

func TestListResidentOrchestratorsIsBindingBased(t *testing.T) {
	d := testDB(t)
	got, err := d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)

	// A legacy user orchestrator named "resident" is not the standing team.
	_, err = d.conn.Exec(`INSERT INTO hera_orchestrators(name,created_at) VALUES ('resident','2026-11-09')`)
	testutil.NoError(t, err)
	got, err = d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)

	_, err = d.conn.Exec(`UPDATE hera_orchestrators SET name='legacy'`)
	testutil.NoError(t, err)
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	got, err = d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, got[0].ID, o.ID)
	// The generic list still hides it.
	all, err := d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	for _, x := range all {
		testutil.False(t, x.ID == o.ID)
	}
}

// A legacy user orchestrator named "resident" that the resident task is NOT
// bound to is a name collision, not the standing team: its workers are not
// resident workers and do not count toward the cap.
func TestResidentWorkerLegacyNameCollisionNotCounted(t *testing.T) {
	d := testDB(t)
	_, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	testutil.NoError(t, d.Add(&model.Task{ID: "legacy-worker", Name: "legacy-worker", Status: model.StatusInProgress, CreatedAt: time.Now()}))
	for _, q := range []string{
		`INSERT INTO hera_orchestrators(id,name,created_at) VALUES (77,'resident','2026-11-09')`,
		`INSERT INTO hera_roles(id,orchestrator_id,name,kind,argus_project,created_at) VALUES (770,77,'lw','worker','p','2026-11-09')`,
		`INSERT INTO hera_bindings(role_id,orchestrator_id,argus_task_id,worktree_path,started_at) VALUES (770,77,'legacy-worker','/tmp/x','2026-11-09')`,
	} {
		_, err := d.conn.Exec(q)
		testutil.NoError(t, err)
	}
	is, err := d.IsResidentWorker("legacy-worker")
	testutil.NoError(t, err)
	testutil.False(t, is)
	n, err := d.ResidentWorkerCount()
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)
}
