package api

import (
	"os"
	"path/filepath"

	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleMoveTaskAccount_PendingGraduateIs409(t *testing.T) {
	for _, body := range []string{`{"account":"work"}`, `{"account":"work","backend":"codex"}`} {
		r := &moveTestRunner{live: &moveTestSession{idle: true}}
		s, task := moveServer(t, r)
		testutil.NoError(t, s.db.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
		w := postMove(s, task.ID, body)
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Contains(t, w.Body.String(), "graduation restart pending")
		testutil.Nil(t, r.kicked)
		testutil.Nil(t, r.recycled)
		testutil.Nil(t, r.started)
		got, err := s.db.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Account, "")
	}
}

// A graduation committing between the pre-check and db.Delete must leave the
// session running and the log intact: Delete runs first and refuses.
func TestHandleDeleteTask_RaceRefusalStopsNothing(t *testing.T) {
	srv, d := testServer(t)
	rec := &stopRecorder{SessionRunner: srv.runner}
	srv.runner = rec
	testutil.NoError(t, d.Add(modelTask("g1")))
	t.Setenv("HOME", t.TempDir())
	logPath := agent.SessionLogPath("g1")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
	testutil.NoError(t, os.WriteFile(logPath, []byte("log"), 0o600))
	artFile := filepath.Join(agent.ArtifactsDir("g1"), "a.txt")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(artFile), 0o755))
	testutil.NoError(t, os.WriteFile(artFile, []byte("art"), 0o600))
	_, err := d.UpsertArtifact(&model.Artifact{TaskID: "g1", Filename: "a.txt"})
	testutil.NoError(t, err)
	deleteTaskAfterPrecheck = func(id string) {
		testutil.NoError(t, d.SetMeta(id, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
	}
	t.Cleanup(func() { deleteTaskAfterPrecheck = nil })
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("DELETE", "/api/tasks/g1", ""))
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Equal(t, len(rec.stopped), 0)
	_, err = d.Get("g1")
	testutil.NoError(t, err)
	_, err = os.Stat(logPath)
	testutil.NoError(t, err)
	_, err = os.Stat(artFile)
	testutil.NoError(t, err)
	arts, err := d.Artifacts("g1")
	testutil.NoError(t, err)
	testutil.Equal(t, len(arts), 1)
}

func TestHandleDeleteTask_ResidentIs409(t *testing.T) {
	srv, d := testServer(t)
	t.Setenv("HOME", t.TempDir())
	rec := &stopRecorder{SessionRunner: srv.runner}
	srv.runner = rec
	res, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	// Routed (resident gate middleware) and direct (handler's own defense in
	// depth, reached when the gate is bypassed) both answer 409, no side effects.
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("DELETE", "/api/tasks/"+res.ID, ""))
	testutil.Equal(t, w.Code, http.StatusConflict)
	w = httptest.NewRecorder()
	req := authedReq("DELETE", "/api/tasks/"+res.ID, "")
	req.SetPathValue("id", res.ID)
	srv.handleDeleteTask(w, req)
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Equal(t, len(rec.stopped), 0)
	got, err := d.ResidentTask()
	testutil.NoError(t, err)
	testutil.True(t, got != nil)
}

func TestHandleDeleteTask_StopsAfterDelete(t *testing.T) {
	srv, d := testServer(t)
	rec := &stopRecorder{SessionRunner: srv.runner}
	srv.runner = rec
	testutil.NoError(t, d.Add(modelTask("g2")))
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("DELETE", "/api/tasks/g2", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.DeepEqual(t, rec.stopped, []string{"g2"})
}

func modelTask(id string) *model.Task {
	return &model.Task{ID: id, Name: id, Status: model.StatusInProgress, Project: "p"}
}
