package db

import (
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func graduationFixture(t *testing.T) (*DB, *HeraBinding) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	task := &model.Task{ID: "graduate", Name: "graduate", Project: "project", Worktree: t.TempDir(), Branch: "argus/resident/work", SessionID: "conversation", Account: "routed", AccountRouted: true}
	testutil.NoError(t, d.Add(task))
	_, b, err := d.CreateResidentWorkerRoleWithBinding(CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: HeraKindWorker, ArgusProject: "project", Prompt: "mission"}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(task.ID, HeraMetaNamespace, HeraMetaKeyResidentWorker, "true"))
	coord, err := d.HeraLiveBindingByTask(res.ID)
	testutil.NoError(t, err)
	_, err = d.SendHeraMessage(coord.RoleID, b.RoleID, "mail", "mail", nil)
	testutil.NoError(t, err)
	return d, b
}

func TestGraduateHeraWorker(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.CreateHeraOrchestrator("work", "")
	testutil.NoError(t, err)
	result, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	o, err := d.HeraOrchestrator(result.NewBinding.OrchestratorID)
	testutil.NoError(t, err)
	testutil.Equal(t, o.Name, "work-2")
	testutil.Equal(t, result.NewRole.Kind, HeraKindCoordinator)
	testutil.Equal(t, result.NewRole.ArgusProject, "project")
	testutil.Equal(t, result.NewRole.Prompt, "mission")
	testutil.Equal(t, result.NewBinding.ArgusTaskID, b.ArgusTaskID)
	testutil.Equal(t, result.NewBinding.WorktreePath, b.WorktreePath)
	history, err := d.ListHeraBindingsByRole(b.RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, history[0].EndReason, HeraEndReasonGraduated)
	mail, err := d.HeraInbox(b.RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(mail), 0)
	yes, err := d.IsResidentWorker(b.ArgusTaskID)
	testutil.NoError(t, err)
	testutil.False(t, yes)
	meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[b.ArgusTaskID][HeraMetaKeyGraduationPendingRestart], "true")
	testutil.Equal(t, meta[b.ArgusTaskID]["graduated_from"], ResidentOrchestratorName)
	task, err := d.Get(b.ArgusTaskID)
	testutil.NoError(t, err)
	testutil.Equal(t, task.SessionID, "conversation")
	testutil.Equal(t, task.Branch, "argus/resident/work")
	testutil.True(t, task.AccountRouted)
	testutil.Equal(t, task.Account, "routed")
	_, err = d.GraduateHeraWorker(b.ID)
	testutil.ErrorIs(t, err, ErrHeraNotFound)
}

func TestGraduateHeraWorkerAtomic(t *testing.T) {
	for _, table := range []string{"hera_bindings:end", "hera_orchestrators", "hera_roles", "hera_bindings", "task_meta", "task_meta:delete", "hera_messages"} {
		t.Run(table, func(t *testing.T) {
			d, b := graduationFixture(t)
			verb, target := "INSERT", table
			switch table {
			case "hera_bindings:end":
				verb, target = "UPDATE", "hera_bindings"
			case "task_meta:delete":
				verb, target = "DELETE", "task_meta"
			case "hera_messages":
				verb = "UPDATE"
			}
			_, err := d.conn.Exec(fmt.Sprintf(`CREATE TRIGGER graduate_failure BEFORE %s ON %s BEGIN SELECT RAISE(FAIL,'injected'); END`, verb, target))
			testutil.NoError(t, err)
			_, err = d.GraduateHeraWorker(b.ID)
			testutil.True(t, err != nil)
			live, err := d.HeraLiveBindingByRole(b.RoleID)
			testutil.NoError(t, err)
			testutil.Equal(t, live.ID, b.ID)
			visible, err := d.ListHeraOrchestrators(false)
			testutil.NoError(t, err)
			testutil.Equal(t, len(visible), 0)
			yes, err := d.IsResidentWorker(b.ArgusTaskID)
			testutil.NoError(t, err)
			testutil.True(t, yes)
		})
	}
}

func TestGraduateHeraWorkerMetaAndLinks(t *testing.T) {
	d, b := graduationFixture(t)
	for _, k := range []string{"ready_to_close", "pending_recycle"} {
		testutil.NoError(t, d.SetMeta(b.ArgusTaskID, HeraMetaNamespace, k, "x"))
	}
	var linksBefore int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_orch_links`).Scan(&linksBefore))
	_, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	kv := meta[b.ArgusTaskID]
	testutil.Equal(t, kv[HeraMetaKeyRole], string(HeraKindCoordinator))
	for _, k := range []string{HeraMetaKeyResidentWorker, "ready_to_close", "pending_recycle"} {
		_, present := kv[k]
		testutil.False(t, present)
	}
	var linksAfter int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_orch_links`).Scan(&linksAfter))
	testutil.Equal(t, linksAfter, linksBefore)
}

func TestGraduateHeraWorkerResidentNameBase(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.conn.Exec(`UPDATE hera_roles SET name='resident-coord' WHERE name=?`, ResidentOrchestratorName)
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`UPDATE hera_roles SET name=? WHERE id=?`, ResidentOrchestratorName, b.RoleID)
	testutil.NoError(t, err)
	result, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	o, err := d.HeraOrchestrator(result.NewBinding.OrchestratorID)
	testutil.NoError(t, err)
	testutil.Equal(t, o.Name, "resident-graduate")
}

func TestGraduateHeraWorkerMultiBindingRefused(t *testing.T) {
	d, b := graduationFixture(t)
	other, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: other.ID, Name: "second", Kind: HeraKindFreelance, ArgusProject: "project"}, b.ArgusTaskID, b.WorktreePath)
	testutil.NoError(t, err)
	_, err = d.GraduateHeraWorker(b.ID)
	testutil.ErrorIs(t, err, ErrHeraAmbiguous)
	live, err := d.HeraLiveBindingByRole(b.RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, live.ID, b.ID)
	yes, err := d.IsResidentWorker(b.ArgusTaskID)
	testutil.NoError(t, err)
	testutil.True(t, yes)
	meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	_, present := meta[b.ArgusTaskID][HeraMetaKeyGraduationPendingRestart]
	testutil.False(t, present)
}
