package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleResumeRestartCanceledLock(t *testing.T) {
	for _, action := range []string{"resume", "restart"} {
		t.Run(action, func(t *testing.T) {
			s, _, _, _, _ := bulkSetup(t)
			unlock, err := s.lockTaskMutation(context.Background(), "busy")
			testutil.NoError(t, err)
			defer unlock()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			request := authedReq("POST", "/api/tasks/busy/"+action, "").WithContext(ctx)
			response := httptest.NewRecorder()
			s.routes().ServeHTTP(response, request)
			testutil.Equal(t, response.Code, http.StatusServiceUnavailable)
		})
	}
}

type lifecycleProbeRunner struct {
	agent.SessionRunner
	stop   func(string) error
	starts int
}

func (r *lifecycleProbeRunner) Stop(id string) error { return r.stop(id) }
func (r *lifecycleProbeRunner) StartOrReattach(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, bool, error) {
	r.starts++
	return ownershipSession{}, false, nil
}

func TestHandleResumeRestartRereadsAfterDeletion(t *testing.T) {
	for _, action := range []string{"resume", "restart"} {
		t.Run(action, func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			task := addTask(t, d, "deleted-first")
			doc := seedDeleteJob(t, d, "deleted-first", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
			runner := &lifecycleProbeRunner{SessionRunner: s.runner}
			s.runner = runner
			synctest.Test(t, func(t *testing.T) {
				stopping, proceed, deleted, responded := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				runner.stop = func(string) error { close(stopping); <-proceed; return nil }
				go func() {
					defer close(deleted)
					testutil.NoError(t, s.processDeleteTask("deleted-first", &doc, &doc.Results[0]))
				}()
				<-stopping
				response := httptest.NewRecorder()
				go func() {
					defer close(responded)
					s.routes().ServeHTTP(response, authedReq("POST", "/api/tasks/"+task.ID+"/"+action, ""))
				}()
				synctest.Wait()
				close(proceed)
				<-deleted
				<-responded
				testutil.Equal(t, response.Code, http.StatusNotFound)
				testutil.Equal(t, runner.starts, 0)
				s.deleteJobCancel()
			})
		})
	}
}

func TestTaskMutationSharesExistingLocker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s Server
		unlock := s.taskLocks.Lock("task")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			release, err := s.lockTaskMutation(ctx, "task")
			if release != nil {
				release()
			}
			testutil.ErrorIs(t, err, context.Canceled)
		}()
		synctest.Wait()
		s.taskLocks.mu.Lock()
		refs := s.taskLocks.m["task"].refs
		s.taskLocks.mu.Unlock()
		testutil.Equal(t, refs, 2)
		cancel()
		<-done
		testutil.Equal(t, s.taskLocks.size(), 1)
		unlock()
		unlock()
		testutil.Equal(t, s.taskLocks.size(), 0)
	})
}
