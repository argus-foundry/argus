package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAPI_RecordSessionID(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{Name: "t", Project: "p", Backend: "codex", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(task))
	const sid = "01a1ebbc-ca71-7c31-b311-b3aa50da12d9"
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := authedReq("POST", path, body)
		req.Header.Set("X-Argus-Auth", "master")
		srv.routes().ServeHTTP(w, req)
		return w
	}

	t.Run("non-master token is refused", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/session-id", `{"session_id":"`+sid+`"}`))
		testutil.Equal(t, w.Code, http.StatusForbidden)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.SessionID, "")
	})

	w := post("/api/tasks/"+task.ID+"/session-id", `{"session_id":"`+sid+`"}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"recorded":true`)
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.SessionID, sid)

	w = post("/api/tasks/"+task.ID+"/session-id", `{"session_id":"22222222-2222-4222-8222-222222222222"}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"recorded":false`)

	// a "clear" start (a /new inside the session) moves the task to the new thread
	const newThread = "33333333-3333-4333-8333-333333333333"
	w = post("/api/tasks/"+task.ID+"/session-id", `{"session_id":"`+newThread+`","source":"clear"}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"recorded":true`)
	got, _ = d.Get(task.ID)
	testutil.Equal(t, got.SessionID, newThread)

	testutil.Equal(t, post("/api/tasks/"+task.ID+"/session-id", `{"session_id":"bad"}`).Code, http.StatusBadRequest)
	testutil.Equal(t, post("/api/tasks/"+task.ID+"/session-id", `{`).Code, http.StatusBadRequest)
	testutil.Equal(t, post("/api/tasks/missing/session-id", `{"session_id":"`+sid+`"}`).Code, http.StatusNotFound)
}
