package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func failureEvents(t *testing.T, sink *recordingSink, id string) []*model.TaskFailure {
	t.Helper()
	var out []*model.TaskFailure
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeTaskFailureChanged || ev.TaskID != id {
			continue
		}
		var p map[string]*model.TaskFailure
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		f, ok := p["failure"]
		testutil.Equal(t, ok, true)
		out = append(out, f)
	}
	return out
}

func failureOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	v, ok := body["failure"]
	testutil.Equal(t, ok, true)
	if v == nil {
		return nil
	}
	return v.(map[string]any)
}

func TestTaskFailure_ProjectionAndEvents(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "f1", Name: "n", Status: model.StatusInReview}))

	_, body := getTaskJSON(t, srv, "f1")
	testutil.Equal(t, failureOf(t, body) == nil, true)

	testutil.NoError(t, d.SetResult("f1", `{"failed":true,"reason":"validation failed"}`))
	code, body := getTaskJSON(t, srv, "f1")
	testutil.Equal(t, code, http.StatusOK)
	f := failureOf(t, body)
	testutil.Equal(t, f["reason"].(string), "validation failed")
	testutil.Equal(t, f["source"].(string), "task_result")
	testutil.Contains(t, f["recorded_at"].(string), "T")

	stored := d.ResultAt("f1")
	testutil.Equal(t, stored.IsZero(), false)
	testutil.Equal(t, f["recorded_at"].(string), stored.UTC().Format("2006-01-02T15:04:05Z07:00"))

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks", ""))
	var list map[string][]taskJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	testutil.Equal(t, list["tasks"][0].Failure.Reason, "validation failed")
	testutil.Equal(t, list["tasks"][0].Failure.RecordedAt, f["recorded_at"].(string))

	evs := failureEvents(t, sink, "f1")
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].Reason, "validation failed")
	testutil.Equal(t, evs[0].RecordedAt, f["recorded_at"].(string))

	testutil.NoError(t, d.SetResult("f1", `{"failed":true,"reason":"validation failed"}`))
	testutil.Equal(t, len(failureEvents(t, sink, "f1")), 1)

	testutil.NoError(t, d.SetResult("f1", `{"failed":true,"reason":"still broken"}`))
	evs = failureEvents(t, sink, "f1")
	testutil.Equal(t, len(evs), 2)
	testutil.Equal(t, evs[1].Reason, "still broken")

	testutil.NoError(t, d.SetResult("f1", `{"pr":"https://x/1"}`))
	_, body = getTaskJSON(t, srv, "f1")
	testutil.Equal(t, failureOf(t, body) == nil, true)
	evs = failureEvents(t, sink, "f1")
	testutil.Equal(t, len(evs), 3)
	testutil.Equal(t, evs[2] == nil, true)
}

func TestTaskFailure_ClearedViaUpdate(t *testing.T) {
	_, d := testServer(t)
	sink := installAPISink(t)
	task := &model.Task{ID: "f2", Name: "n", Result: `{"failed":true,"reason":"x"}`}
	testutil.NoError(t, d.Add(task))
	task.Result = ""
	testutil.NoError(t, d.Update(task))
	evs := failureEvents(t, sink, "f2")
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0] == nil, true)
}

func TestTaskFailure_NotFabricated(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)

	cases := map[string]string{
		"malformed":      `{"failed":tru`,
		"array":          `[1,2]`,
		"scalar":         `"failed"`,
		"failed string":  `{"failed":"true","reason":"x"}`,
		"failed false":   `{"failed":false,"reason":"x"}`,
		"unrelated":      `{"pr":"u"}`,
		"exit text only": `exit status 1`,
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			id := "nf-" + strings.ReplaceAll(name, " ", "-")
			testutil.NoError(t, d.Add(&model.Task{ID: id, Name: "n", Status: model.StatusInReview, Result: res}))
			code, body := getTaskJSON(t, srv, id)
			testutil.Equal(t, code, http.StatusOK)
			testutil.Equal(t, failureOf(t, body) == nil, true)
			testutil.Equal(t, len(failureEvents(t, sink, id)), 0)
			testutil.NoError(t, d.SetResult(id, res+" "))
			testutil.Equal(t, len(failureEvents(t, sink, id)), 0)
		})
	}
}

func TestTaskFailure_StopAndExitAreNotFailures(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)
	task := &model.Task{ID: "stp", Name: "n", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	startLiveSleep(t, srv, d, task)

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/stp/stop", ""))
	testutil.Equal(t, w.Code, http.StatusOK)

	_, body := getTaskJSON(t, srv, "stp")
	testutil.Equal(t, failureOf(t, body) == nil, true)
	testutil.Equal(t, len(failureEvents(t, sink, "stp")), 0)
}

func TestTaskFailure_ReasonBounded(t *testing.T) {
	long := strings.Repeat("é", model.MaxFailureReasonBytes)
	raw, _ := json.Marshal(map[string]any{"failed": true, "reason": long})
	reason, ok := model.ParseTaskFailure(string(raw))
	testutil.Equal(t, ok, true)
	testutil.Equal(t, len(reason) <= model.MaxFailureReasonBytes, true)
	testutil.Equal(t, strings.ToValidUTF8(reason, "") == reason, true)
}

func TestTaskFailure_HeraBoundSameProjection(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "hb", Name: "n", Status: model.StatusInReview}))
	testutil.NoError(t, d.SetResult("hb", `{"failed":true}`))
	_, body := getTaskJSON(t, srv, "hb")
	f := failureOf(t, body)
	testutil.Equal(t, f["reason"].(string), "")
	testutil.Equal(t, f["source"].(string), "task_result")
}

func TestTaskFailure_UnknownTask404AndAuth(t *testing.T) {
	srv, _ := testServer(t)
	code, _ := getTaskJSON(t, srv, "missing")
	testutil.Equal(t, code, http.StatusNotFound)
	h := authMiddleware("test-token", nil, nil, srv.routes(), "/public")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/missing", nil))
	testutil.Equal(t, w.Code, http.StatusUnauthorized)
}
