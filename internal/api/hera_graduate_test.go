package api

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleHeraGraduateVisibleAsTopLevelCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, d := testServer(t)
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "graduate", Name: "graduate", Project: "p", Branch: "argus/resident/work", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	_, b, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	result, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	resp := getHera(t, srv)
	testutil.Equal(t, len(resp.Orchestrators), 1)
	testutil.Equal(t, resp.Orchestrators[0].ID, result.NewRole.OrchestratorID)
	testutil.Equal(t, len(resp.Orchestrators[0].Roles), 1)
	r := resp.Orchestrators[0].Roles[0]
	testutil.Equal(t, r.RoleID, result.NewRole.ID)
	testutil.Equal(t, r.Kind, string(db.HeraKindCoordinator))
	testutil.Equal(t, r.TaskID, w.ID)
	testutil.True(t, r.Live)
}
