package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func assertTaskPinSnapshot(t *testing.T, srv *Server, id string, pinned, archived bool) {
	t.Helper()
	stored, err := srv.db.Get(id)
	testutil.NoError(t, err)
	testutil.Equal(t, stored.Pinned, pinned)
	testutil.Equal(t, stored.Archived, archived)

	for _, path := range []string{"/api/tasks?archived=all", "/api/tasks/" + id} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq(http.MethodGet, path, ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var snapshot map[string]any
			if path == "/api/tasks?archived=all" {
				var response struct {
					Tasks []map[string]any `json:"tasks"`
				}
				testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				for _, task := range response.Tasks {
					if task["id"] == id {
						snapshot = task
						break
					}
				}
			} else {
				testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
			}
			testutil.NotNil(t, snapshot)
			value, present := snapshot["pinned"]
			testutil.True(t, present)
			testutil.DeepEqual(t, value, any(pinned))
			testutil.Equal(t, snapshot["archived"] == true, archived)
		})
	}
}

func TestTaskSnapshotsPinned(t *testing.T) {
	srv, d := testServer(t)
	cases := []struct {
		id     string
		pinned bool
	}{
		{"pinned", true},
		{"unpinned", false},
	}
	for _, tc := range cases {
		testutil.NoError(t, d.Add(&model.Task{ID: tc.id, Name: tc.id, Pinned: tc.pinned}))
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			assertTaskPinSnapshot(t, srv, tc.id, tc.pinned, false)
		})
	}
}

func TestTaskSnapshotsPinLifecycle(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "pin-lifecycle", Name: "pin lifecycle"}))
	for _, tc := range []struct {
		name, action, body string
		pinned, archived   bool
	}{
		{"pin", "pin", `{"pinned":true}`, true, false},
		{"unpin", "pin", `{"pinned":false}`, false, false},
		{"repin", "pin", `{"pinned":true}`, true, false},
		{"archive clears pin", "archive", "", false, true},
		{"unpin leaves archive", "pin", `{"pinned":false}`, false, true},
		{"pin restores", "pin", `{"pinned":true}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/pin-lifecycle/"+tc.action, tc.body))
			testutil.Equal(t, w.Code, http.StatusOK)
			assertTaskPinSnapshot(t, srv, "pin-lifecycle", tc.pinned, tc.archived)
		})
	}
}
