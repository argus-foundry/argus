package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type observingDeleteRunner struct {
	agent.SessionRunner
	stop func(string) error
}

func (r observingDeleteRunner) Stop(id string) error { return r.stop(id) }

type observingDeleteOps struct {
	cleanup func(string, string, string) agent.GitCleanupResult
}

func (o observingDeleteOps) IsDirty(string) (bool, error)                  { return false, nil }
func (o observingDeleteOps) Cleanup(w, b, r string) agent.GitCleanupResult { return o.cleanup(w, b, r) }

func seedDeleteJob(t *testing.T, d *db.DB, id string, results ...storedDeleteResult) storedDeleteJob {
	t.Helper()
	doc := storedDeleteJob{Results: results}
	raw, err := json.Marshal(doc)
	testutil.NoError(t, err)
	_, _, err = d.CreateTaskDeleteJob(db.TaskDeleteJob{ID: id, RequestHash: id, Status: deleteJobQueued, Results: string(raw)})
	testutil.NoError(t, err)
	return doc
}
func execDeleteSQL(t *testing.T, d *db.DB, query string, args ...any) {
	t.Helper()
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error { _, err := tx.Exec(query, args...); return err }))
}
func TestBulkDelete_CompleteBody(t *testing.T) {
	for _, tail := range []string{` {}`, ` garbage`, strings.Repeat(" ", 65537)} {
		t.Run(fmt.Sprint(len(tail), tail[:2]), func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			task := addTask(t, d, "keep")
			w := postBulk(t, s, `{"task_ids":["`+task.ID+`"]}`+tail)
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			jobs, err := d.ListUnfinishedTaskDeleteJobs()
			testutil.NoError(t, err)
			testutil.Equal(t, len(jobs), 0)
			_, err = d.Get(task.ID)
			testutil.NoError(t, err)
		})
	}
}
func TestBulkDelete_StopBeforeDestruction(t *testing.T) {
	for _, stopErr := range []error{nil, errors.New("supervisor unavailable"), agent.ErrSessionNotFound, errors.New(agent.ErrSessionNotFound.Error())} {
		t.Run(fmt.Sprint(stopErr), func(t *testing.T) {
			s, d, ops, _, _ := bulkSetup(t)
			task := addTask(t, d, "stop")
			log := agent.SessionLogPath(task.ID)
			artifact := filepath.Join(agent.ArtifactsDir(task.ID), "keep")
			testutil.NoError(t, os.MkdirAll(filepath.Dir(log), 0700))
			testutil.NoError(t, os.WriteFile(log, []byte("log"), 0600))
			testutil.NoError(t, os.MkdirAll(filepath.Dir(artifact), 0700))
			testutil.NoError(t, os.WriteFile(artifact, []byte("artifact"), 0600))
			stopped := false
			s.runner = observingDeleteRunner{s.runner, func(id string) error {
				_, err := d.Get(id)
				testutil.NoError(t, err)
				for _, p := range []string{log, artifact} {
					_, err := os.Stat(p)
					testutil.NoError(t, err)
				}
				stopped = true
				return stopErr
			}}
			s.deleteOps = observingDeleteOps{func(w, b, r string) agent.GitCleanupResult {
				testutil.True(t, stopped)
				_, err := d.Get(task.ID)
				testutil.ErrorIs(t, err, db.ErrTaskNotFound)
				return ops.Cleanup(w, b, r)
			}}

			v := waitJob(t, s, submit(t, s, `{"task_ids":["`+task.ID+`"]}`))
			testutil.True(t, stopped)
			if stopErr != nil && !errors.Is(stopErr, agent.ErrSessionNotFound) && stopErr.Error() != agent.ErrSessionNotFound.Error() {
				testutil.Equal(t, v.Results[0].State, deleteTaskFailed)
				testutil.Equal(t, v.Results[0].RecordDeleted, false)
				_, err := d.Get(task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, len(ops.cleanups), 0)
				for _, p := range []string{log, artifact} {
					_, err := os.Stat(p)
					testutil.NoError(t, err)
				}
			} else {
				testutil.Equal(t, v.Results[0].State, deleteTaskSucceeded)
			}
		})
	}
}
func TestBulkDelete_CheckpointFailure(t *testing.T) {
	s, d, ops, rec, _ := bulkSetup(t)
	task := addTask(t, d, "checkpoint")
	doc := seedDeleteJob(t, d, "checkpoint", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	execDeleteSQL(t, d, `CREATE TRIGGER reject_snapshot BEFORE UPDATE ON task_delete_jobs WHEN NEW.results LIKE '%"snap"%' BEGIN SELECT RAISE(FAIL,'checkpoint unavailable'); END`)
	testutil.Error(t, s.processDeleteTask("checkpoint", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	if doc.Results[0].Error != nil {
		testutil.Equal(t, *doc.Results[0].Error, "snapshot_save_failed")
	}
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(rec.stopped), 0)
	testutil.Equal(t, len(ops.cleanups), 0)
	execDeleteSQL(t, d, `DROP TRIGGER reject_snapshot`)
	s.runDeleteJob("checkpoint")
	job, err := d.GetTaskDeleteJob("checkpoint")
	testutil.NoError(t, err)
	testutil.NoError(t, json.Unmarshal([]byte(job.Results), &doc))
	testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
	testutil.Equal(t, len(ops.cleanups), 1)
}
func TestBulkDelete_OwnershipScanFailsClosed(t *testing.T) {
	for _, broken := range []string{"row", "query"} {
		t.Run(broken, func(t *testing.T) {
			s, d, ops, _, _ := bulkSetup(t)
			survivor := addTask(t, d, "survivor")
			res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{Worktree: survivor.Worktree, Branch: survivor.Branch}}
			if broken == "row" {
				execDeleteSQL(t, d, `UPDATE tasks SET agent_pid='invalid' WHERE id=?`, survivor.ID)
			} else {
				testutil.NoError(t, d.Close())
			}
			s.cleanupDeletedTask(&res, ops)
			testutil.Equal(t, len(ops.cleanups), 0)
			testutil.Equal(t, res.State, deleteTaskFailed)
		})
	}
}
func TestBulkDelete_OwnershipAliasesAndBranches(t *testing.T) {
	for _, claim := range []string{"clean alias", "symlink alias", "branch", "base branch", "remote base branch"} {
		t.Run(claim, func(t *testing.T) {
			s, d, ops, _, _ := bulkSetup(t)
			survivor := addTask(t, d, "survivor")
			wt, br := survivor.Worktree, "argus/old"
			switch claim {
			case "clean alias":
				survivor.Worktree = wt + "/."
			case "symlink alias":
				alias := filepath.Join(t.TempDir(), "alias")
				testutil.NoError(t, os.Symlink(wt, alias))
				survivor.Worktree = alias
			case "branch":
				survivor.Worktree = ""
				survivor.Branch = br
			case "base branch":
				survivor.Worktree = ""
				survivor.BaseBranch = br
			case "remote base branch":
				survivor.Worktree = ""
				survivor.BaseBranch = "origin/" + br
			}
			testutil.NoError(t, d.Update(survivor))
			res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{Worktree: wt, Branch: br}}
			s.cleanupDeletedTask(&res, ops)
			want := wt + "|"
			if strings.Contains(claim, "alias") {
				want = "|"
			}
			testutil.DeepEqual(t, ops.cleanups, []string{want})
		})
	}
}
func TestBulkDelete_MaximumProgressEvents(t *testing.T) {
	s, d, _, _, sink := bulkSetup(t)
	ids := make([]string, 100)
	for i := range ids {
		task := &model.Task{Name: fmt.Sprint(i), Status: model.StatusInReview}
		testutil.NoError(t, d.Add(task))
		ids[i] = task.ID
	}
	b, err := json.Marshal(bulkDeleteReq{TaskIDs: ids})
	testutil.NoError(t, err)
	id := submit(t, s, string(b))
	s.deleteJobWG.Wait()
	waitJob(t, s, id)
	count := 0
	for _, ev := range sink.events() {
		if ev.Type == model.EventTypeTaskDeleteJobUpdated {
			count++
		}
	}
	testutil.Equal(t, count, 102)
}
func TestBulkDelete_ShutdownQuiesces(t *testing.T) {
	s, d, _, _, _ := bulkSetup(t)
	first, second := addTask(t, d, "first"), addTask(t, d, "second")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.deleteOps = observingDeleteOps{func(w, b, r string) agent.GitCleanupResult {
		once.Do(func() { close(entered) })
		<-release
		return agent.GitCleanupResult{}
	}}
	id := submit(t, s, `{"task_ids":["`+first.ID+`","`+second.ID+`"]}`)
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	testutil.ErrorIs(t, s.Shutdown(ctx), context.Canceled)
	close(release)
	s.deleteJobWG.Wait()
	_, err := d.Get(second.ID)
	testutil.NoError(t, err)
	job, err := d.GetTaskDeleteJob(id)
	testutil.NoError(t, err)
	testutil.Equal(t, job.Status, deleteJobRunning)
}

func TestBulkDelete_DurableCheckpointRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "jobs.sqlite")
	d, err := db.Open(path)
	testutil.NoError(t, err)
	task := addTask(t, d, "durable")
	ops := &fakeDeleteOps{}
	s := &Server{db: d, runner: agent.NewRunner(nil), deleteOps: ops}
	doc := seedDeleteJob(t, d, "durable", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	execDeleteSQL(t, d, `CREATE TRIGGER crash_after_delete BEFORE UPDATE ON task_delete_jobs WHEN NEW.results LIKE '%"record_deleted":true%' BEGIN SELECT RAISE(FAIL,'interrupted after delete'); END`)
	testutil.Error(t, s.processDeleteTask("durable", &doc, &doc.Results[0]))
	_, err = d.Get(task.ID)
	testutil.ErrorIs(t, err, db.ErrTaskNotFound)
	testutil.Equal(t, len(ops.cleanups), 0)
	job, err := d.GetTaskDeleteJob("durable")
	testutil.NoError(t, err)
	var checkpoint storedDeleteJob
	testutil.NoError(t, json.Unmarshal([]byte(job.Results), &checkpoint))
	testutil.Equal(t, checkpoint.Results[0].RecordDeleted, false)
	testutil.True(t, checkpoint.Results[0].Snap != nil)
	execDeleteSQL(t, d, `DROP TRIGGER crash_after_delete`)
	testutil.NoError(t, d.Close())
	reopened, err := db.Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { testutil.NoError(t, reopened.Close()) })
	freshOps := &fakeDeleteOps{}
	fresh := &Server{db: reopened, runner: agent.NewRunner(nil), deleteOps: freshOps}
	fresh.runDeleteJob("durable")
	testutil.DeepEqual(t, freshOps.cleanups, []string{task.Worktree + "|" + task.Branch})
	job, err = reopened.GetTaskDeleteJob("durable")
	testutil.NoError(t, err)
	testutil.NoError(t, json.Unmarshal([]byte(job.Results), &checkpoint))
	testutil.Equal(t, checkpoint.Results[0].RecordDeleted, true)
	testutil.Equal(t, checkpoint.Results[0].State, deleteTaskSucceeded)
}

func TestBulkDelete_ShutdownDuringStop(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	task := addTask(t, d, "shutdown-stop")
	entered, release := make(chan struct{}), make(chan struct{})
	s.runner = observingDeleteRunner{s.runner, func(string) error { close(entered); <-release; return nil }}
	id := submit(t, s, `{"task_ids":["`+task.ID+`"]}`)
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	testutil.ErrorIs(t, s.Shutdown(ctx), context.Canceled)
	close(release)
	s.deleteJobWG.Wait()
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(ops.cleanups), 0)
	job, err := d.GetTaskDeleteJob(id)
	testutil.NoError(t, err)
	testutil.Equal(t, job.Status, deleteJobRunning)
	testutil.Equal(t, postBulk(t, s, `{"task_ids":["missing"]}`).Code, http.StatusServiceUnavailable)
}
