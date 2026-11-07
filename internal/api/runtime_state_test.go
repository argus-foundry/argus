package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func startLiveSleep(t *testing.T, srv *Server, d *db.DB, task *model.Task) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts a real PTY-backed sleep; skipped in -short")
	}
	testutil.NoError(t, d.SetBackend("sh-sleep", config.Backend{Command: "sleep 30"}))
	task.Backend = "sh-sleep"
	task.Worktree = t.TempDir()
	if got, _ := d.Get(task.ID); got == nil {
		testutil.NoError(t, d.Add(task))
	}
	sess, err := srv.runner.Start(task, d.Config(), 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.runner.Stop(task.ID)
		<-sess.Done()
	})
}

func getTaskJSON(t *testing.T, srv *Server, id string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+id, ""))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestTaskRuntime_LiveIndependentOfStatus(t *testing.T) {
	for _, st := range []model.Status{model.StatusPending, model.StatusInProgress, model.StatusInReview, model.StatusComplete} {
		t.Run(st.String(), func(t *testing.T) {
			srv, d := testServer(t)
			task := &model.Task{ID: "rt-" + st.String(), Name: "n", Status: st, Project: "p"}
			testutil.NoError(t, d.Add(task))

			code, body := getTaskJSON(t, srv, task.ID)
			testutil.Equal(t, code, http.StatusOK)
			testutil.Equal(t, body["live"].(bool), false)
			_, hasIdle := body["idle"]
			testutil.Equal(t, hasIdle, false)

			startLiveSleep(t, srv, d, task)
			code, body = getTaskJSON(t, srv, task.ID)
			testutil.Equal(t, code, http.StatusOK)
			testutil.Equal(t, body["live"].(bool), true)
			_, hasIdle = body["idle"]
			testutil.Equal(t, hasIdle, false)
			testutil.Equal(t, body["status"].(string), st.String())
		})
	}
}

func TestTaskRuntime_ListMatchesGet(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{ID: "rt-list", Name: "n", Status: model.StatusInReview, Project: "p"}
	testutil.NoError(t, d.Add(task))
	startLiveSleep(t, srv, d, task)

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var resp map[string][]taskJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.Equal(t, len(resp["tasks"]), 1)
	testutil.Equal(t, resp["tasks"][0].Live, true)
	testutil.Equal(t, resp["tasks"][0].Idle, false)
}

func TestTaskRuntime_PendingRestartNeverLive(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{ID: "rt-restart", Name: "n", Status: model.StatusInReview, Project: "p"}
	testutil.NoError(t, d.Add(task))
	startLiveSleep(t, srv, d, task)
	srv.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, true)
	t.Cleanup(func() { srv.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, false) })

	_, body := getTaskJSON(t, srv, task.ID)
	testutil.Equal(t, body["live"].(bool), false)
	_, hasIdle := body["idle"]
	testutil.Equal(t, hasIdle, false)
	_, hasNeeds := body["needs_input"]
	testutil.Equal(t, hasNeeds, false)
}

func TestTaskRuntime_UnknownTask404(t *testing.T) {
	srv, _ := testServer(t)
	code, _ := getTaskJSON(t, srv, "nope")
	testutil.Equal(t, code, http.StatusNotFound)
}

func TestTaskRuntime_RequiresAuth(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "auth1", Name: "n"}))
	for _, path := range []string{"/api/tasks", "/api/tasks/auth1"} {
		h := authMiddleware("test-token", nil, nil, srv.routes(), "/public")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	}
}

func runtimeEvents(t *testing.T, sink *recordingSink, id string) []runtimeSig {
	t.Helper()
	var out []runtimeSig
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeSessionRuntimeChanged || ev.TaskID != id {
			continue
		}
		var p runtimeSig
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		out = append(out, p)
	}
	return out
}

func TestEmitRuntimeChanges(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)
	state := newIdleWatcherState()

	task := &model.Task{ID: "rc1", Name: "n", Status: model.StatusInReview, Project: "p"}
	testutil.NoError(t, d.Add(task))

	srv.emitRuntimeChanges(state)
	testutil.Equal(t, len(runtimeEvents(t, sink, "rc1")), 0)

	startLiveSleep(t, srv, d, task)
	srv.emitRuntimeChanges(state)
	srv.emitRuntimeChanges(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, "rc1"), []runtimeSig{{Live: true}})

	srv.runner.(*agent.Runner).SetPendingRestartForTest("rc1", true)
	srv.emitRuntimeChanges(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, "rc1"), []runtimeSig{{Live: true}, {}})

	srv.runner.(*agent.Runner).SetPendingRestartForTest("rc1", false)
	srv.emitRuntimeChanges(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, "rc1"), []runtimeSig{{Live: true}, {}, {Live: true}})
}

func TestEmitRuntimeChanges_StatusTransitionSuppressesNeedsInput(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)
	state := newIdleWatcherState()

	task := &model.Task{ID: "rc2", Name: "n", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(task))
	startLiveSleep(t, srv, d, task)
	srv.runner.SetNeedsInputIDs([]string{"rc2"})
	srv.emitRuntimeChanges(state)

	testutil.NoError(t, d.SetStatus("rc2", model.StatusInReview))
	srv.emitRuntimeChanges(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, "rc2"), []runtimeSig{{Live: true, NeedsInput: true}, {Live: true}})
}
