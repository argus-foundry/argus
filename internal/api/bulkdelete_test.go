package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type fakeDeleteOps struct {
	mu       sync.Mutex
	dirty    map[string]bool
	cleanups []string
	results  map[string]agent.GitCleanupResult
}

func (f *fakeDeleteOps) IsDirty(wt string) (bool, error) { return f.dirty[wt], nil }

func (f *fakeDeleteOps) Cleanup(wt, branch, repo string) agent.GitCleanupResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, wt+"|"+branch)
	if r, ok := f.results[branch]; ok {
		return r
	}
	return agent.GitCleanupResult{Worktree: agent.CleanupRemoved, LocalBranch: agent.CleanupRemoved, RemoteBranch: agent.CleanupAbsent}
}

type stopRecorder struct {
	agent.SessionRunner
	mu      sync.Mutex
	stopped []string
}

func (s *stopRecorder) Stop(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, id)
	return nil
}

func bulkSetup(t *testing.T) (*Server, *db.DB, *fakeDeleteOps, *stopRecorder, *recordingSink) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { testutil.NoError(t, d.Close()) })
	rec := &stopRecorder{SessionRunner: agent.NewRunner(nil)}
	srv := &Server{db: d, runner: rec, stopCh: make(chan struct{})}
	ops := &fakeDeleteOps{dirty: map[string]bool{}, results: map[string]agent.GitCleanupResult{}}
	srv.deleteOps = ops
	sink := &recordingSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() {
		testutil.NoError(t, srv.Shutdown(context.Background()))
		srv.deleteJobWG.Wait()
		events.SetSink(prev)
	})
	return srv, d, ops, rec, sink
}

func addTask(t *testing.T, d *db.DB, name string) *model.Task {
	t.Helper()
	wt := filepath.Join(t.TempDir(), ".argus", "worktrees", "p", name)
	testutil.NoError(t, os.MkdirAll(wt, 0o755))
	task := &model.Task{Name: name, Branch: "argus/" + name, Worktree: wt, Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	return task
}

func postBulk(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/bulk-delete", body))
	return w
}

type jobView struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Results   []struct {
		TaskID        string  `json:"task_id"`
		State         string  `json:"state"`
		RecordDeleted bool    `json:"record_deleted"`
		Worktree      string  `json:"worktree"`
		LocalBranch   string  `json:"local_branch"`
		RemoteBranch  string  `json:"remote_branch"`
		Error         *string `json:"error"`
	} `json:"results"`
}

func waitJob(t *testing.T, srv *Server, jobID string) jobView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("GET", "/api/task-delete-jobs/"+jobID, ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		var v jobView
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
		if v.Status == "complete" {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete")
	return jobView{}
}

func submit(t *testing.T, srv *Server, body string) string {
	t.Helper()
	w := postBulk(t, srv, body)
	testutil.Equal(t, w.Code, http.StatusAccepted)
	var out map[string]string
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out["job_id"]
}

func TestBulkDelete_Validation(t *testing.T) {
	srv, _, _, _, _ := bulkSetup(t)
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "id" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	over, _ := json.Marshal(map[string]any{"task_ids": ids})
	tests := []struct{ name, body string }{
		{"malformed", `{`},
		{"empty list", `{"task_ids":[]}`},
		{"missing list", `{}`},
		{"over 100", string(over)},
		{"duplicates", `{"task_ids":["a","a"]}`},
		{"blank id", `{"task_ids":[" "]}`},
		{"unknown field", `{"task_ids":["a"],"bogus":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, postBulk(t, srv, tt.body).Code, http.StatusBadRequest)
		})
	}
	t.Run("exactly 100 accepted", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"task_ids": ids[:100]})
		waitJob(t, srv, submit(t, srv, string(b)))
	})
}

func TestBulkDelete_UnknownJob(t *testing.T) {
	srv, _, _, _, _ := bulkSetup(t)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/task-delete-jobs/nope", ""))
	testutil.Equal(t, w.Code, http.StatusNotFound)
}

func TestBulkDelete_Idempotency(t *testing.T) {
	srv, d, _, _, _ := bulkSetup(t)
	a := addTask(t, d, "a")
	body := `{"task_ids":["` + a.ID + `"],"idempotency_key":"k1"}`
	first := submit(t, srv, body)
	testutil.Equal(t, submit(t, srv, body), first)
	waitJob(t, srv, first)

	w := postBulk(t, srv, `{"task_ids":["other"],"idempotency_key":"k1"}`)
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Contains(t, w.Body.String(), "idempotency_conflict")

	different := submit(t, srv, `{"task_ids":["x"],"idempotency_key":"k2"}`)
	testutil.Equal(t, different == first, false)
}

func TestBulkDelete_SuccessAndPartialFailure(t *testing.T) {
	srv, d, ops, rec, sink := bulkSetup(t)
	good, bad := addTask(t, d, "good"), addTask(t, d, "bad")
	ops.results[bad.Branch] = agent.GitCleanupResult{
		Worktree: agent.CleanupRemoved, LocalBranch: agent.CleanupFailed, RemoteBranch: agent.CleanupAbsent, Err: "local branch still exists",
	}
	job := submit(t, srv, `{"task_ids":["`+good.ID+`","`+bad.ID+`"]}`)
	v := waitJob(t, srv, job)

	testutil.Equal(t, v.Total, 2)
	testutil.Equal(t, v.Completed, 2)
	testutil.Equal(t, v.Results[0].State, "succeeded")
	testutil.Equal(t, v.Results[0].RecordDeleted, true)
	testutil.Equal(t, v.Results[0].Worktree, "removed")
	testutil.Equal(t, v.Results[0].RemoteBranch, "absent")
	testutil.True(t, v.Results[0].Error == nil)
	testutil.Equal(t, v.Results[1].State, "failed")
	testutil.Equal(t, v.Results[1].RecordDeleted, true)
	testutil.Equal(t, v.Results[1].LocalBranch, "failed")
	testutil.Equal(t, *v.Results[1].Error, "local branch still exists")

	testutil.DeepEqual(t, rec.stopped, []string{good.ID, bad.ID})
	for _, id := range []string{good.ID, bad.ID} {
		_, err := d.Get(id)
		testutil.True(t, err != nil)
	}

	var deleted, jobEvents int
	var last map[string]any
	for _, ev := range sink.events() {
		switch ev.Type {
		case model.EventTypeTaskDeleted:
			deleted++
		case model.EventTypeTaskDeleteJobUpdated:
			jobEvents++
			testutil.NoError(t, json.Unmarshal(ev.Payload, &last))
		}
	}
	testutil.Equal(t, deleted, 2)
	testutil.True(t, jobEvents >= 2)
	testutil.Equal(t, last["job_id"].(string), job)
	testutil.Equal(t, last["status"].(string), "complete")
	testutil.Equal(t, int(last["completed"].(float64)), 2)
	testutil.Equal(t, int(last["total"].(float64)), 2)
}

func TestBulkDelete_SkipsProtectedTasks(t *testing.T) {
	srv, d, ops, rec, _ := bulkSetup(t)
	dirty, prOpen, coord, clean := addTask(t, d, "dirty"), addTask(t, d, "propen"), addTask(t, d, "coord"), addTask(t, d, "clean")
	ops.dirty[dirty.Worktree] = true
	testutil.NoError(t, d.SetMeta(prOpen.ID, "pr", "state", "approved"))
	testutil.NoError(t, d.SetMeta(clean.ID, "pr", "state", "merged-closed"))
	orch, err := d.CreateHeraOrchestrator("o", "master")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator}, coord.ID, coord.Worktree)
	testutil.NoError(t, err)

	body := `{"task_ids":["` + dirty.ID + `","` + prOpen.ID + `","` + coord.ID + `","missing","` + clean.ID + `"],"preconditions":{"require_pr_closed":true}}`
	v := waitJob(t, srv, submit(t, srv, body))

	want := []struct{ state, code string }{
		{"skipped", "dirty_worktree"}, {"skipped", "pr_not_closed"}, {"skipped", "hera_cascade_required"},
		{"skipped", "task_not_found"}, {"succeeded", ""},
	}
	for i, w := range want {
		testutil.Equal(t, v.Results[i].State, w.state)
		if w.code != "" {
			testutil.Equal(t, *v.Results[i].Error, w.code)
			testutil.Equal(t, v.Results[i].RecordDeleted, false)
			testutil.Equal(t, v.Results[i].Worktree, "pending")
		}
	}
	for _, tk := range []*model.Task{dirty, prOpen, coord} {
		got, err := d.Get(tk.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.ID, tk.ID)
	}
	testutil.DeepEqual(t, rec.stopped, []string{clean.ID})
	testutil.Equal(t, len(ops.cleanups), 1)
}

func TestBulkDelete_AllowDirtyDeletesDirtyTask(t *testing.T) {
	srv, d, ops, _, _ := bulkSetup(t)
	tk := addTask(t, d, "dirty")
	ops.dirty[tk.Worktree] = true
	v := waitJob(t, srv, submit(t, srv, `{"task_ids":["`+tk.ID+`"],"preconditions":{"allow_dirty":true}}`))
	testutil.Equal(t, v.Results[0].State, "succeeded")
}

func TestBulkDelete_RequestContextCancelDoesNotCancelJob(t *testing.T) {
	srv, d, _, _, _ := bulkSetup(t)
	tk := addTask(t, d, "a")
	ctx, cancel := context.WithCancel(context.Background())
	req := authedReq("POST", "/api/tasks/bulk-delete", `{"task_ids":["`+tk.ID+`"]}`).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	cancel()
	var out map[string]string
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	v := waitJob(t, srv, out["job_id"])
	testutil.Equal(t, v.Results[0].State, "succeeded")
}

func TestBulkDelete_ResumeAfterRestartFinishesCleanupOnly(t *testing.T) {
	srv, d, ops, rec, _ := bulkSetup(t)
	gone := storedDeleteResult{
		deleteResultView: deleteResultView{TaskID: "gone", State: deleteTaskRunning, RecordDeleted: true, Worktree: "pending", LocalBranch: "pending", RemoteBranch: "pending"},
		Snap:             &deleteSnap{Worktree: "/tmp/.argus/worktrees/p/gone", Branch: "argus/gone"},
	}
	untouched := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "untouched", State: deleteTaskPending, Worktree: "pending", LocalBranch: "pending", RemoteBranch: "pending"}}
	raw, _ := json.Marshal(storedDeleteJob{Results: []storedDeleteResult{gone, untouched}})
	_, _, err := d.CreateTaskDeleteJob(db.TaskDeleteJob{ID: "tdj_resume", RequestHash: "h", Status: "running", Results: string(raw)})
	testutil.NoError(t, err)

	srv.resumeDeleteJobs()
	v := waitJob(t, srv, "tdj_resume")
	testutil.Equal(t, v.Results[0].State, "succeeded")
	testutil.Equal(t, v.Results[1].State, "skipped")
	testutil.Equal(t, len(rec.stopped), 0)
	testutil.DeepEqual(t, ops.cleanups, []string{"/tmp/.argus/worktrees/p/gone|argus/gone"})

	srv.resumeDeleteJobs()
	srv.deleteJobWG.Wait()
	testutil.Equal(t, len(ops.cleanups), 1)
}

func TestBulkDelete_ResumeNeverTouchesReusedPaths(t *testing.T) {
	srv, d, ops, _, _ := bulkSetup(t)
	reborn := addTask(t, d, "reuse")
	gone := storedDeleteResult{
		deleteResultView: deleteResultView{TaskID: "old", State: deleteTaskRunning, RecordDeleted: true},
		Snap:             &deleteSnap{Worktree: reborn.Worktree, Branch: reborn.Branch},
	}
	raw, _ := json.Marshal(storedDeleteJob{Results: []storedDeleteResult{gone}})
	_, _, err := d.CreateTaskDeleteJob(db.TaskDeleteJob{ID: "tdj_reuse", RequestHash: "h", Status: "running", Results: string(raw)})
	testutil.NoError(t, err)
	srv.resumeDeleteJobs()
	waitJob(t, srv, "tdj_reuse")
	testutil.DeepEqual(t, ops.cleanups, []string{"|"})
}
