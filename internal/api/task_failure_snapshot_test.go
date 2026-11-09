package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type resultSnapshotRunner struct {
	agent.SessionRunner
	afterRead func()
}

func (r *resultSnapshotRunner) RunningAndIdle() ([]string, []string) {
	r.afterRead()
	return nil, nil
}

func TestTaskFailure_ResultTimestampSnapshot(t *testing.T) {
	for _, path := range []string{"/api/tasks/snapshot", "/api/tasks"} {
		t.Run(path, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv, d := testServer(t)
				testutil.NoError(t, srv.Shutdown(context.Background()))
				synctest.Wait()
				task := &model.Task{ID: "snapshot", Name: "n", Result: `{"failed":true,"reason":"first"}`}
				testutil.NoError(t, d.Add(task))
				firstAt := d.ResultAt(task.ID).UTC().Format(time.RFC3339)
				srv.runner = &resultSnapshotRunner{SessionRunner: srv.runner, afterRead: func() {
					time.Sleep(2 * time.Second)
					testutil.NoError(t, d.SetResult(task.ID, `{"failed":true,"reason":"second"}`))
				}}
				w := httptest.NewRecorder()
				srv.routes().ServeHTTP(w, authedReq("GET", path, ""))
				testutil.Equal(t, w.Code, http.StatusOK)
				var body map[string]any
				testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				if path == "/api/tasks" {
					body = body["tasks"].([]any)[0].(map[string]any)
				}
				f := failureOf(t, body)
				testutil.Equal(t, f["reason"], "first")
				testutil.Equal(t, f["recorded_at"].(string), firstAt)
			})
		})
	}
}

func TestTaskFailure_UpdateTimestampMatchesEventAndREST(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, srv.Shutdown(context.Background()))
		synctest.Wait()
		sink := installAPISink(t)
		task := &model.Task{ID: "update-time", Name: "n", Result: `{"failed":true,"reason":"first"}`}
		testutil.NoError(t, d.Add(task))
		firstAt := d.ResultAt(task.ID)
		time.Sleep(2 * time.Second)
		task.Result = `{"failed":true,"reason":"second"}`
		testutil.NoError(t, d.Update(task))
		at := d.ResultAt(task.ID)
		testutil.Equal(t, at.After(firstAt), true)
		_, body := getTaskJSON(t, srv, task.ID)
		f := failureOf(t, body)
		testutil.Equal(t, f["reason"], "second")
		testutil.Equal(t, f["recorded_at"].(string), at.UTC().Format(time.RFC3339))
		evs := failureEvents(t, sink, task.ID)
		testutil.DeepEqual(t, evs, []*model.TaskFailure{{Reason: "second", Source: "task_result", RecordedAt: at.UTC().Format(time.RFC3339)}})
	})
}
