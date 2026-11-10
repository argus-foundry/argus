package hera

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// residentWorkerNukeFixture seeds the standing team with one resident worker.
func residentWorkerNukeFixture(t *testing.T, task *model.Task) (*db.DB, *db.HeraOrchestrator, *heramodel.RoleView) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: task.Name, Kind: db.HeraKindWorker, ArgusProject: task.Project}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return d, o, &heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true, Kind: db.HeraKindWorker}
}

func TestNukeResidentWorker_SecondLiveBindingPreservesTask(t *testing.T) {
	task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
	d, _, view := residentWorkerNukeFixture(t, task)
	o2, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	r2, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o2.ID, Name: "w2", Kind: db.HeraKindWorker, ArgusProject: "p"})
	testutil.NoError(t, err)
	b2, err := d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: r2.ID, OrchestratorID: o2.ID, ArgusTaskID: task.ID})
	testutil.NoError(t, err)

	followUp, err := NukeResidentWorker(d, nil, view)
	testutil.NoError(t, err)
	followUp()
	role, err := d.HeraRole(view.RoleID)
	testutil.NoError(t, err)
	testutil.True(t, role.NukedAt != nil)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, got.Archived)
	live, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, live.ID, b2.ID)
}

func TestNukeResidentWorker_Refusals(t *testing.T) {
	t.Run("pending graduation", func(t *testing.T) {
		task := &model.Task{ID: "g", Name: "g", Project: "p", Status: model.StatusInProgress}
		d, _, view := residentWorkerNukeFixture(t, task)
		testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
		_, err := NukeResidentWorker(d, nil, view)
		testutil.ErrorIs(t, err, db.ErrGraduationPending)
		role, err := d.HeraRole(view.RoleID)
		testutil.NoError(t, err)
		testutil.True(t, role.NukedAt == nil)
		got, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.False(t, got.Archived)
	})
	t.Run("resident target", func(t *testing.T) {
		task := &model.Task{ID: "w", Name: "w", Project: "p", Status: model.StatusInProgress}
		d, _, _ := residentWorkerNukeFixture(t, task)
		res, err := d.ResidentTask()
		testutil.NoError(t, err)
		b, err := d.HeraLiveBindingByTask(res.ID)
		testutil.NoError(t, err)
		_, err = NukeResidentWorker(d, nil, &heramodel.RoleView{RoleID: b.RoleID, TaskID: res.ID, Live: true})
		if err == nil {
			t.Fatal("deleting the resident itself must be refused")
		}
		after, err := d.HeraLiveBindingByTask(res.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, after.ID, b.ID)
	})
}

// The followUp must really re-discover stacked branches off the deleted task's
// branch: a child whose tip is merged into the default branch is deleted.
func TestNukeResidentWorker_FollowUpDeletesMergedStackedBranch(t *testing.T) {
	task := &model.Task{ID: "x", Name: "x", Project: "proj", Branch: "x-branch", Status: model.StatusInProgress, CreatedAt: mustParseTime(t, "2026-01-01T00:00:00Z")}
	d, _, view := residentWorkerNukeFixture(t, task)
	dir := initGitRepo(t, t.TempDir())
	gitRun(t, dir, "checkout", "-b", "x-branch")
	addGitCommit(t, dir, "x.txt", "x1\n")
	gitRun(t, dir, "checkout", "-b", "c-branch")
	addGitCommit(t, dir, "c.txt", "c1\n")
	gitRun(t, dir, "checkout", "-b", "tip-branch")
	addGitCommit(t, dir, "tip.txt", "tip1\n")
	gitRun(t, dir, "checkout", "master")
	gitRun(t, dir, "merge", "--no-ff", "tip-branch", "-m", "merge tip")
	gitRun(t, dir, "update-ref", "refs/remotes/origin/x-branch", "x-branch")
	gitRun(t, dir, "update-ref", "refs/remotes/origin/c-branch", "c-branch")
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: dir}))
	child := &model.Task{Name: "c", Project: "proj", Branch: "c-branch", BaseBranch: "x-branch", CreatedAt: mustParseTime(t, "2026-01-02T00:00:00Z")}
	testutil.NoError(t, d.Add(child))
	tip := &model.Task{Name: "tip", Project: "proj", Branch: "tip-branch", BaseBranch: "c-branch", CreatedAt: mustParseTime(t, "2026-01-03T00:00:00Z")}
	testutil.NoError(t, d.Add(tip))

	followUp, err := NukeResidentWorker(d, nil, view)
	testutil.NoError(t, err)
	testutil.True(t, gitBranchExists(dir, "c-branch")) // not before the follow-up
	followUp()
	testutil.False(t, gitBranchExists(dir, "c-branch"))
	testutil.False(t, gitBranchExists(dir, "x-branch"))
}

func TestNukeFailClosedOnUnreadablePendingCheck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{ID: "t1", Name: "t1", Project: "p", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	o, err := d.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p"}, task.ID, "")
	testutil.NoError(t, err)
	view := heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true, Kind: db.HeraKindWorker}
	subtree := []*heramodel.OrchView{{ID: o.ID, Name: "o", Roles: []heramodel.RoleView{view}}}
	// Make only the pending-marker read fail; every other table stays readable.
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error { _, e := tx.Exec(`DROP TABLE task_meta`); return e }))

	err = CheckNukeTask(d, task.ID)
	if err == nil {
		t.Fatal("unreadable pending check must refuse")
	}
	testutil.False(t, errors.Is(err, db.ErrGraduationPending))

	ids, bg, err := NukeSubtree(d, nil, subtree)
	if err == nil {
		t.Fatal("cascade must refuse on an unreadable check")
	}
	testutil.False(t, errors.Is(err, db.ErrGraduationPending))
	bg()
	testutil.Equal(t, len(ids), 0)
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt == nil)

	// The resident variant surfaces the real error, not ErrGraduationPending.
	_, err = NukeResidentWorker(d, nil, &view)
	if err == nil {
		t.Fatal("NukeResidentWorker must refuse on an unreadable check")
	}
	testutil.False(t, errors.Is(err, db.ErrGraduationPending))
	r, err = d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt == nil)
}
