package api

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRuntimeRejectedCloseoutPreservesRevision(t *testing.T) {
	srv, d := testServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
	task.Backend = "claude"
	task.Worktree = t.TempDir()
	task.SessionID = "11111111-1111-7111-9111-111111111111"
	testutil.NoError(t, d.Update(task))
	testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
	testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
	seedAPITranscript(t, home, task.Worktree, task.SessionID, time.Now().Add(-time.Hour))
	seedAPITranscript(t, home, task.Worktree, "22222222-2222-7222-9222-222222222222", time.Now())
	runner := &guardRunner{SessionRunner: srv.runner, startErr: errors.New("must not start")}
	srv.runner = runner
	_, rev := runtimeGet(t, srv, task.ID)
	sink := installAPISink(t)
	w := runtimePatch(srv, task.ID, `{"prompt":"updated","apply":"restart"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Equal(t, decodeBody(t, w)["code"], any("hera_closeout"))
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.SessionID, task.SessionID)
	testutil.Equal(t, runtimeRevision(got), rev)
	testutil.Equal(t, runner.calls, 0)
	testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
}

type runtimeRevisionRunner struct {
	agent.SessionRunner
	edit func()
}

func (r *runtimeRevisionRunner) HasPendingRestart(id string) bool {
	if r.edit != nil {
		edit := r.edit
		r.edit = nil
		edit()
	}
	return r.SessionRunner.HasPendingRestart(id)
}

func TestRuntimeWriteChecksRevision(t *testing.T) {
	for _, field := range []string{"model", "session"} {
		t.Run(field, func(t *testing.T) {
			srv, d := testServer(t)
			task := addStopped(t, d, nil)
			_, rev := runtimeGet(t, srv, task.ID)
			srv.runner = &runtimeRevisionRunner{SessionRunner: srv.runner, edit: func() {
				fresh, err := d.Get(task.ID)
				testutil.NoError(t, err)
				if field == "model" {
					fresh.Model = "opus"
				} else {
					fresh.SessionID = "independent-session"
				}
				testutil.NoError(t, d.Update(fresh))
			}}
			sink := installAPISink(t)
			w := runtimePatch(srv, task.ID, `{"prompt":"updated","apply":"next_resume"}`, ifMatch(rev))
			testutil.Equal(t, w.Code, http.StatusPreconditionFailed)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Prompt, "orig prompt")
			if field == "model" {
				testutil.Equal(t, got.Model, "opus")
			} else {
				testutil.Equal(t, got.SessionID, "independent-session")
			}
			testutil.Equal(t, decodeBody(t, w)["revision"], any(runtimeRevision(got)))
			testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
		})
	}
}

func TestRuntimeConcurrentPatches(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, body := range []string{`{"model":"opus","apply":"next_resume"}`, `{"model":"sonnet","apply":"next_resume"}`} {
		wg.Go(func() { <-start; results <- runtimePatch(srv, task.ID, body, ifMatch(rev)).Code })
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	testutil.Equal(t, counts[http.StatusOK], 1)
	testutil.Equal(t, counts[http.StatusPreconditionFailed], 1)
}

func TestRuntimeSessionInvalidatesRevision(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	task.SessionID = "captured-session"
	testutil.NoError(t, d.Update(task))
	w := runtimePatch(srv, task.ID, `{"model":"opus","apply":"next_resume"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusPreconditionFailed)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Model, "")
	testutil.Equal(t, got.SessionID, task.SessionID)
}

func TestRuntimeAdmittedRestartCommitsDiscoveredSession(t *testing.T) {
	srv, d := testServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	task := addStopped(t, d, func(task *model.Task) { task.SessionID = "11111111-1111-7111-9111-111111111111" })
	sid := "22222222-2222-7222-9222-222222222222"
	seedAPITranscript(t, home, task.Worktree, sid, time.Now())
	runner := &guardRunner{SessionRunner: srv.runner, startErr: errors.New("injected launch failure")}
	srv.runner = runner
	_, rev := runtimeGet(t, srv, task.ID)
	w := runtimePatch(srv, task.ID, `{"prompt":"updated","apply":"restart"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusInternalServerError)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.SessionID, sid)
	testutil.Equal(t, got.Prompt, "updated")
	testutil.Equal(t, runner.calls, 1)
	testutil.Equal(t, decodeBody(t, w)["revision"], any(runtimeRevision(got)))
}
