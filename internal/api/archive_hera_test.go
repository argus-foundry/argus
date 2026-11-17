package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// heraBoundTask seeds orchestrator + coordinator + an in_review worker task.
func heraBoundTask(t *testing.T, d *db.DB) (*model.Task, *db.HeraRole) {
	t.Helper()
	orch, err := d.CreateHeraOrchestrator("O", "master")
	testutil.NoError(t, err)
	ct := &model.Task{Name: "coord", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(ct))
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, ct.ID, "/wt/c")
	testutil.NoError(t, err)
	task := &model.Task{Name: "w", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/w", Branch: "b"}
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p"}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(task.ID, model.StatusInReview))
	return task, role
}

func archivePost(srv *Server, id, action, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/"+action, body))
	return w
}

func TestArchiveEndpoint_HeraBoundTask(t *testing.T) {
	gitBad := func(*model.Task) (bool, string) { return false, "open PR" }
	handoffBad := func(*model.Task) (hera.HandoffInfo, bool, string) { return hera.HandoffInfo{}, false, "CI failing" }
	handoffOK := func(*model.Task) (hera.HandoffInfo, bool, string) {
		return hera.HandoffInfo{PRURL: "https://x/pr/1", TipSHA: "abc"}, true, ""
	}

	t.Run("refusal is a 409 carrying the hera reason and the override hint", func(t *testing.T) {
		srv, d := testServer(t)
		srv.SetArchiveGuard(nil, gitBad, handoffBad)
		task, _ := heraBoundTask(t, d)
		w := archivePost(srv, task.ID, "archive", "")
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Contains(t, w.Body.String(), "CI failing")
		testutil.Contains(t, w.Body.String(), "force=true")
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
	})
	t.Run("passing verdict archives role and task together", func(t *testing.T) {
		srv, d := testServer(t)
		srv.SetArchiveGuard(nil, gitBad, handoffOK)
		task, role := heraBoundTask(t, d)
		w := archivePost(srv, task.ID, "archive", "")
		testutil.Equal(t, w.Code, http.StatusOK)
		var out map[string]any
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		testutil.Equal(t, out["kind"], "handed_off")
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
		r, _ := d.HeraRole(role.ID)
		testutil.Equal(t, r.ArchivedAt != nil, true)

		// Unarchive brings the role back too.
		w = archivePost(srv, task.ID, "unarchive", "")
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ = d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
		r, _ = d.HeraRole(role.ID)
		testutil.Nil(t, r.ArchivedAt)
	})
	t.Run("force with a reason overrides; force without one is a 400", func(t *testing.T) {
		srv, d := testServer(t)
		srv.SetArchiveGuard(nil, gitBad, handoffBad)
		task, _ := heraBoundTask(t, d)
		w := archivePost(srv, task.ID, "archive", `{"force":true}`)
		testutil.Equal(t, w.Code, http.StatusBadRequest)
		w = archivePost(srv, task.ID, "archive", `{"force":true,"reason":"human override"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
	})
	t.Run("an unwired guard fails closed", func(t *testing.T) {
		srv, d := testServer(t)
		task, _ := heraBoundTask(t, d)
		w := archivePost(srv, task.ID, "archive", "")
		testutil.Equal(t, w.Code, http.StatusConflict)
	})
	t.Run("plain task archives as before", func(t *testing.T) {
		srv, d := testServer(t)
		task := &model.Task{Name: "plain", Status: model.StatusComplete}
		testutil.NoError(t, d.Add(task))
		w := archivePost(srv, task.ID, "archive", "")
		testutil.Equal(t, w.Code, http.StatusOK)
	})
}
