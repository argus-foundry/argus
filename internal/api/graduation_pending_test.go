package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// Only the graduation watcher may restart a pending graduate: a runtime
// change (which kicks a live session) or a Claude session switch must refuse
// before persisting anything, and fail closed when the state is unreadable.
func TestRestartRoutesRefusePendingGraduate(t *testing.T) {
	routes := []struct {
		name, method, path, body string
	}{
		{"runtime/next_resume", "PATCH", "/api/tasks/g1/runtime", `{"apply":"next_resume","model":"opus"}`},
		{"runtime/restart", "PATCH", "/api/tasks/g1/runtime", `{"apply":"restart","model":"opus"}`},
		{"session switch", "POST", "/api/tasks/g1/claude-session", `{"session_id":"other"}`},
	}
	for _, rt := range routes {
		t.Run(rt.name+"/pending", func(t *testing.T) {
			srv, d := testServer(t)
			rec := &stopRecorder{SessionRunner: srv.runner}
			srv.runner = rec
			testutil.NoError(t, d.Add(&model.Task{ID: "g1", Name: "g1", Status: model.StatusInProgress, Project: "p", Backend: "claude", SessionID: "orig"}))
			testutil.NoError(t, d.SetMeta("g1", db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))
			w := httptest.NewRecorder()
			req := authedReq(rt.method, rt.path, rt.body)
			stored, err := d.Get("g1")
			testutil.NoError(t, err)
			// A valid precondition and a real field change: only the guard
			// stands between this request and the persist/restart.
			req.Header.Set("If-Match", runtimeRevision(stored))
			srv.routes().ServeHTTP(w, req)
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Contains(t, w.Body.String(), "graduation restart pending")
			testutil.Equal(t, len(rec.stopped), 0)
			got, err := d.Get("g1")
			testutil.NoError(t, err)
			testutil.Equal(t, got.SessionID, "orig")
			testutil.Equal(t, got.Model, "")
			testutil.True(t, d.GraduationPending("g1"))
		})
		t.Run(rt.name+"/unreadable", func(t *testing.T) {
			srv, d := testServer(t)
			testutil.NoError(t, d.Add(&model.Task{ID: "g1", Name: "g1", Status: model.StatusInProgress, Project: "p", Backend: "claude", SessionID: "orig"}))
			testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error { _, err := tx.Exec(`DROP TABLE task_meta`); return err }))
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq(rt.method, rt.path, rt.body))
			testutil.Equal(t, w.Code, http.StatusInternalServerError)
			got, err := d.Get("g1")
			testutil.NoError(t, err)
			testutil.Equal(t, got.SessionID, "orig")
		})
	}
	t.Run("not pending passes the guard", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "g1", Name: "g1", Status: model.StatusInProgress, Project: "p"}))
		w := httptest.NewRecorder()
		testutil.False(t, srv.refuseGraduationPending(w, "g1", "test"))
		testutil.Equal(t, w.Code, http.StatusOK)
	})
}
