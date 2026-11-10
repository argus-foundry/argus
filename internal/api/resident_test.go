package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResident_RESTHeraExcludesStandingTeam(t *testing.T) {
	srv, d := testServer(t)
	res, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	task := &model.Task{ID: "errand", Name: "errand", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(task))
	_, _, err = d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "errand", Kind: db.HeraKindWorker}, task.ID, "/wt/errand")
	testutil.NoError(t, err)
	w := serve(srv.routes(), "GET", "/api/hera", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.False(t, strings.Contains(w.Body.String(), `"name":"resident"`))
	testutil.False(t, strings.Contains(w.Body.String(), `"name":"errand"`))
	w = serve(srv.routes(), "GET", "/api/tasks", "")
	testutil.Contains(t, w.Body.String(), "errand")
}

func TestResidentWaitingUsesStickyDetector(t *testing.T) {
	srv, d := testServer(t)
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

// A pending outward-action confirmation is Claude's numbered permission prompt;
// the existing detector must flag the resident's session for it, and the
// resident-specific waiting signal must be derived from that flag
// (add-resident-agent D6/D7).
func TestResident_PendingConfirmationRaisesWaiting(t *testing.T) {
	srv, d := testServer(t)
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)

	// REAL captured Claude Code bytes (not hand-written): a trimmed session-log
	// tail of a worker parked on a built-in Bash tool permission prompt
	// ("❯ 1. Yes / 2. No", "Tab to amend" footer), copied unmodified from
	// internal/tui/testdata/needsinput/bash_perm_prompt_1.bin (captured from a
	// live Argus worker session, committed in #1065 after review). It is a Bash
	// prompt, not an MCP-tool one: no MCP prompt capture exists locally, and the
	// chooser layout the detector keys on is the same. Rendered at the 88x48
	// size it was captured at.
	prompt, err := os.ReadFile(filepath.Join("testdata", "resident_bash_perm_prompt.bin"))
	testutil.NoError(t, err)
	sizeOf := func(string) (int, int) { return 88, 48 }
	tailOf := func(string) []byte { return prompt }
	noInput := func(string) time.Time { return time.Time{} }
	notArchived := func(string) bool { return false }
	screen := &agent.ScreenRenderer{}
	// Tick 1 records the fingerprint of the parked prompt; tick 2 flags it.
	_, fp, _, _, _, _ := computeNeedsInput(nil, []string{r.ID}, nil, nil, nil, nil, nil, nil, tailOf, noInput, notArchived, screen, sizeOf)
	needs, _, _, _, _, _ := computeNeedsInput(nil, []string{r.ID}, nil, fp, nil, nil, nil, nil, tailOf, noInput, notArchived, screen, sizeOf)
	testutil.DeepEqual(t, needs, []string{r.ID})
	testutil.Equal(t, srv.containsResident(needs), true)
	testutil.Equal(t, len(srv.withoutResident(needs)), 0)
}

func TestResident_BulkDeleteRefusesBatchWithResident(t *testing.T) {
	mux, resID := residentFixture(t)
	w := serve(mux, "POST", "/api/tasks/bulk-delete", `{"task_ids":["plain","`+resID+`"]}`)
	testutil.Equal(t, w.Code, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "resident") {
		t.Fatalf("expected a resident refusal, got %s", w.Body.String())
	}
}
