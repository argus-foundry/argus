package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestResidentWaitingUsesStickyDetector(t *testing.T) {
	srv, d := testServer(t)
	close(srv.stopCh)
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	state := newIdleWatcherState()
	tail := blockedTail
	tailOf := func(string) []byte { return tail }
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, tailOf)
	testutil.Equal(t, srv.ResidentWaiting(), true)
	testutil.Equal(t, srv.runner.(*agent.Runner).NeedsInput(r.ID), false)
	// A prompt disappearing from the tail while busy retains sticky waiting.
	tail = idleTail
	srv.detectNeedsInputTick(state, []string{r.ID}, nil, tailOf)
	testutil.Equal(t, srv.ResidentWaiting(), true)
	testutil.Equal(t, srv.runner.(*agent.Runner).NeedsInput(r.ID), false)
	// A stopped session clears the same signal without a second detector.
	srv.detectNeedsInputTick(state, nil, nil, tailOf)
	testutil.Equal(t, srv.ResidentWaiting(), false)
}

func residentFixture(t *testing.T) (http.Handler, string) {
	t.Helper()
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "plain", Name: "plain", Status: model.StatusInProgress}))
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	return srv.routes(), r.ID
}

func serve(mux http.Handler, method, url, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq(method, url, body))
	return w
}

func TestResident_RESTListAndStatusExcludeIt(t *testing.T) {
	mux, resID := residentFixture(t)

	w := serve(mux, "GET", "/api/tasks", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	if strings.Contains(w.Body.String(), resID) {
		t.Fatalf("/api/tasks leaked the resident: %s", w.Body.String())
	}

	w = serve(mux, "GET", "/api/tasks-raw", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	if strings.Contains(w.Body.String(), resID) {
		t.Fatalf("/api/tasks-raw leaked the resident: %s", w.Body.String())
	}

	w = serve(mux, "GET", "/api/status", "")
	var st statusResponse
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	testutil.Equal(t, st.Tasks.InProgress, 1)
}

func TestResident_RESTGetIsNotFound(t *testing.T) {
	mux, resID := residentFixture(t)
	for _, path := range []string{"/api/tasks/" + resID, "/api/tasks/" + resID + "/raw"} {
		w := serve(mux, "GET", path, "")
		testutil.Equal(t, w.Code, http.StatusNotFound)
	}
	w := serve(mux, "GET", "/api/tasks/plain", "")
	testutil.Equal(t, w.Code, http.StatusOK)
}

func TestResident_RESTLifecycleVerbsRejected(t *testing.T) {
	mux, resID := residentFixture(t)
	cases := []struct{ method, suffix, body string }{
		{"DELETE", "", ""},
		{"POST", "/archive", ""},
		{"POST", "/unarchive", ""},
		{"POST", "/status", `{"status":"complete"}`},
		{"POST", "/fork", `{}`},
		{"POST", "/account", `{"account":"work"}`},
		{"POST", "/rename", `{"name":"x"}`},
		{"POST", "/pin", ""},
		{"PUT", "/raw", `{"id":"` + resID + `","name":"resident","status":"complete"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			w := serve(mux, tc.method, "/api/tasks/"+resID+tc.suffix, tc.body)
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Contains(t, w.Body.String(), "resident")
		})
	}
}

func TestResident_RESTRawAddCannotCreateOne(t *testing.T) {
	srv, d := testServer(t)
	mux := srv.routes()
	w := serve(mux, "POST", "/api/tasks-raw", `{"name":"x","kind":"resident","status":"in_progress"}`)
	testutil.Equal(t, w.Code, http.StatusConflict)
	r, _ := d.ResidentTask()
	if r != nil {
		t.Fatal("resident row must not exist")
	}
}

func TestResident_RESTRawUpdateCannotPromote(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "p", Name: "p", Status: model.StatusInProgress}))
	w := serve(srv.routes(), "PUT", "/api/tasks/p/raw", `{"id":"p","name":"p","status":"in_progress","kind":"resident"}`)
	testutil.Equal(t, w.Code, http.StatusConflict)
}

func TestResident_NeedsInputSetExcludesResident(t *testing.T) {
	srv, d := testServer(t)
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)

	got := srv.withoutResident([]string{"a", r.ID, "b"})
	testutil.DeepEqual(t, got, []string{"a", "b"})
	testutil.DeepEqual(t, srv.withoutResident(nil), []string(nil))
	testutil.DeepEqual(t, srv.withoutResident([]string{"a"}), []string{"a"})
}
