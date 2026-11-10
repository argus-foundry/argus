package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentExcludedFromNuke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	b, err := d.HeraLiveBindingByTask(res.ID)
	testutil.NoError(t, err)
	view := &heramodel.RoleView{RoleID: b.RoleID, TaskID: res.ID, Live: true, Kind: db.HeraKindCoordinator}
	subtree := []*heramodel.OrchView{{ID: o.ID, Name: o.Name, Roles: []heramodel.RoleView{*view}}}
	p := PreviewNukeSubtree(d, subtree)
	testutil.Equal(t, p.Orchestrators, 0)
	testutil.Equal(t, p.Worktrees, 0)
	ids, background := NukeSubtree(d, nil, subtree)
	background()
	testutil.Equal(t, len(ids), 0)
	NukeSingleRole(d, nil, view)()
	after, err := d.HeraLiveBindingByTask(res.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.ID, b.ID)
}

func TestNukeStaleWorkerPreservesGraduate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{ID: "w", Name: "w", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	old, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	role, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: old.ID, Name: "w", Kind: db.HeraKindWorker}, task.ID, "")
	testutil.NoError(t, err)
	graduated, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	live := graduated.NewBinding
	NukeSingleRole(d, nil, &heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true})()
	after, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, after.Archived)
	b, err = d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, b.ID, live.ID)
}

func TestNukeSkipsPendingGraduate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{ID: "g", Name: "g", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	old, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	_, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: old.ID, Name: "g", Kind: db.HeraKindWorker}, task.ID, "")
	testutil.NoError(t, err)
	grad, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	view := &heramodel.RoleView{RoleID: grad.NewRole.ID, TaskID: task.ID, Live: true, Kind: db.HeraKindCoordinator}
	orch := &heramodel.OrchView{ID: grad.NewRole.OrchestratorID, Name: "g", Roles: []heramodel.RoleView{*view}}

	assertIntact := func(t *testing.T) {
		t.Helper()
		after, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.False(t, after.Archived)
		live, err := d.HeraLiveBindingByTask(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, live.ID, grad.NewBinding.ID)
		testutil.True(t, d.GraduationPending(task.ID))
	}

	NukeSingleRole(d, nil, view)()
	assertIntact(t)
	ids, bg := NukeSubtree(d, nil, []*heramodel.OrchView{orch})
	bg()
	testutil.Equal(t, len(ids), 0)
	assertIntact(t)

	testutil.NoError(t, d.CompleteHeraGraduation(task.ID, "orientation"))
	NukeSingleRole(d, nil, view)()
	after, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, after.Archived)
}
