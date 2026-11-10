package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
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
	_, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
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

func TestPendingGraduateArchiveDeleteRefusedWith409(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, d := testServer(t)
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "graduate", Name: "graduate", Project: "p", Branch: "argus/resident/work", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	_, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	_, err = d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)

	mux := srv.routes()
	for _, tc := range []struct{ name, method, path string }{
		{"archive", "POST", "/api/tasks/graduate/archive"},
		{"delete", "DELETE", "/api/tasks/graduate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedReq(tc.method, tc.path, ""))
			testutil.Equal(t, rec.Code, http.StatusConflict)
			testutil.Contains(t, rec.Body.String(), "graduation restart pending")
			got, err := d.Get("graduate")
			testutil.NoError(t, err)
			testutil.False(t, got.Archived)
			testutil.True(t, d.GraduationPending("graduate"))
		})
	}

	testutil.NoError(t, d.CompleteHeraGraduation("graduate", "orientation"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authedReq("POST", "/api/tasks/graduate/archive", ""))
	testutil.Equal(t, rec.Code, http.StatusOK)
}

// pendingGraduateOrch graduates a resident worker and adds a live sibling worker
// to the graduate's new orchestrator.
func pendingGraduateOrch(t *testing.T, d *db.DB) (grad *db.MoveHeraBindingResult, sib *model.Task, sibRole *db.HeraRole) {
	t.Helper()
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "graduate", Name: "graduate", Project: "p", Branch: "argus/resident/work", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	_, b, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "mission"}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	grad, err = d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	sib = &model.Task{ID: "sib", Name: "sib", Project: "p", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(sib))
	sibRole, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: grad.NewRole.OrchestratorID, Name: "sib", Kind: db.HeraKindWorker, ArgusProject: "p"}, sib.ID, "/tmp/wt-sib")
	testutil.NoError(t, err)
	return grad, sib, sibRole
}

func TestHeraNukeRefusedWith409ForPendingGraduate(t *testing.T) {
	srv, d := testServer(t)
	grad, sib, sibRole := pendingGraduateOrch(t, d)

	t.Run("orchestrator cascade refuses as a whole", func(t *testing.T) {
		w := heraPost(srv, idp("orchestrators", grad.NewRole.OrchestratorID, "nuke"), "", "master")
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Contains(t, w.Body.String(), "graduation restart pending")
		o, err := d.HeraOrchestrator(grad.NewRole.OrchestratorID)
		testutil.NoError(t, err)
		testutil.True(t, o.NukedAt == nil)
		sr, err := d.HeraRole(sibRole.ID)
		testutil.NoError(t, err)
		testutil.True(t, sr.NukedAt == nil)
		got, err := d.Get(sib.ID)
		testutil.NoError(t, err)
		testutil.False(t, got.Archived)
	})
	t.Run("single role refuses", func(t *testing.T) {
		w := heraPost(srv, idp("roles", grad.NewRole.ID, "nuke"), "", "master")
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Contains(t, w.Body.String(), "graduation restart pending")
		gr, err := d.HeraRole(grad.NewRole.ID)
		testutil.NoError(t, err)
		testutil.True(t, gr.NukedAt == nil)
		_, err = d.HeraLiveBindingByRole(grad.NewRole.ID)
		testutil.NoError(t, err)
	})
	t.Run("sibling still nukable alone", func(t *testing.T) {
		w := heraPost(srv, idp("roles", sibRole.ID, "nuke"), "", "master")
		testutil.Equal(t, w.Code, http.StatusOK)
	})
}

func TestBulkDelete_SkipsPendingGraduate(t *testing.T) {
	srv, d, ops, rec, _ := bulkSetup(t)
	tk := addTask(t, d, "grad")
	testutil.NoError(t, d.SetMeta(tk.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
	v := waitJob(t, srv, submit(t, srv, `{"task_ids":["`+tk.ID+`"]}`))
	testutil.Equal(t, v.Results[0].State, "skipped")
	testutil.Equal(t, *v.Results[0].Error, "graduation_pending")
	testutil.Equal(t, len(rec.stopped), 0)
	testutil.Equal(t, len(ops.cleanups), 0)
	_, err := os.Stat(tk.Worktree)
	testutil.NoError(t, err)
	got, err := d.Get(tk.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.ID, tk.ID)
}

func TestRawPutArchiveRefusedForPendingGraduate(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "g1", Name: "g1", Status: model.StatusInProgress, Project: "p"}))
	testutil.NoError(t, d.SetMeta("g1", db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
	body := `{"id":"g1","name":"g1","status":"in_progress","project":"p","archived":true,"branch":"","prompt":"","created_at":"2026-05-22T00:00:00Z"}`
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, masterReq("PUT", "/api/tasks/g1/raw", body))
	testutil.Equal(t, w.Code, http.StatusConflict)
	got, err := d.Get("g1")
	testutil.NoError(t, err)
	testutil.False(t, got.Archived)
	testutil.True(t, d.GraduationPending("g1"))
}

func TestDeleteTaskPendingGraduateNoSideEffects(t *testing.T) {
	srv, d := testServer(t)
	rec := &stopRecorder{SessionRunner: agent.NewRunner(nil)}
	srv.runner = rec
	testutil.NoError(t, d.Add(&model.Task{ID: "g2", Name: "g2", Status: model.StatusInProgress, Project: "p"}))
	testutil.NoError(t, d.SetMeta("g2", db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
	logPath := agent.SessionLogPath("g2")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
	testutil.NoError(t, os.WriteFile(logPath, []byte("session"), 0o644))

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("DELETE", "/api/tasks/g2", ""))
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Equal(t, len(rec.stopped), 0)
	_, err := os.Stat(logPath)
	testutil.NoError(t, err)
	got, err := d.Get("g2")
	testutil.NoError(t, err)
	testutil.Equal(t, got.ID, "g2")
}

// writeNukeRefusal's 500 branch: an unreadable pending-marker check refuses
// (fail closed) with "graduation check failed" and nothing is nuked.
func TestHeraNukeRefusedWith500WhenPendingCheckUnreadable(t *testing.T) {
	srv, d := testServer(t)
	orch, roles, tasks := seedOrch(t, d, "o1", db.HeraKindCoordinator, db.HeraKindWorker)
	// Only the pending-marker read can fail: the hera model build and every
	// other table stay readable.
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error { _, err := tx.Exec(`DROP TABLE task_meta`); return err }))

	for name, path := range map[string]string{
		"role": idp("roles", roles[1].ID, "nuke"),
		"orch": idp("orchestrators", orch.ID, "nuke"),
	} {
		t.Run(name, func(t *testing.T) {
			w := heraPost(srv, path, "", "master")
			testutil.Equal(t, w.Code, http.StatusInternalServerError)
			testutil.Contains(t, w.Body.String(), "graduation check failed")
			r, err := d.HeraRole(roles[1].ID)
			testutil.NoError(t, err)
			testutil.True(t, r.NukedAt == nil)
			got, err := d.Get(tasks[1].ID)
			testutil.NoError(t, err)
			testutil.False(t, got.Archived)
		})
	}
}
