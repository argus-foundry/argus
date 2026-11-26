package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/voice"
)

func TestMissingSessionMutationDoesNotCreateLockFiles(t *testing.T) {
	s, _ := testServer(t)
	path := filepath.Join(t.TempDir(), "data.sql")
	d, err := db.Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	s.db = d

	for _, action := range []string{"stop", "resume", "restart"} {
		t.Run(action, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/missing/"+action, ""))
			testutil.Equal(t, w.Code, http.StatusNotFound)
		})
	}
	_, err = os.Stat(path + ".session-locks")
	testutil.ErrorIs(t, err, os.ErrNotExist)
}

func TestSessionMutationsRefuseWhileNativeTUIHoldsLock(t *testing.T) {
	s, d := testServer(t)
	task := &model.Task{Name: "Review", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	unlock, acquired, err := d.TryLockSessionMutation(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	defer unlock()

	for _, action := range []string{"stop", "resume", "restart"} {
		t.Run(action, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/"+task.ID+"/"+action, ""))
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Contains(t, w.Body.String(), "agent_restarting")
		})
	}
	ctx := context.Background()
	op := s.VoiceOperator()
	for _, err := range []error{op.Stop(ctx, task.ID), op.Resume(ctx, task.ID, false)} {
		var opErr *voice.OpError
		if !errors.As(err, &opErr) {
			t.Fatalf("expected a voice operation error, got %v", err)
		}
		testutil.Equal(t, opErr.Code, "agent_restarting")
	}
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInReview)
}

func TestRESTStopRefusesReservedSession(t *testing.T) {
	s, d := testServer(t)
	run := agent.NewRunner(nil)
	s.runner = run
	task := &model.Task{Name: "Review", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	run.SetSessionForTest(task.ID, nil)

	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/"+task.ID+"/stop", ""))
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Contains(t, w.Body.String(), "agent_restarting")
}

func TestResumeRefusesDrainingStoppedSession(t *testing.T) {
	s, d := testServer(t)
	run := agent.NewRunner(nil)
	s.runner = run
	t.Cleanup(run.StopAll)
	// The trap has to run in the session leader. BuildCmd wraps the backend
	// command in `sh -c`, and a tree stop signals that process group: a trap
	// inside a child script does not keep the leader alive, so resume can
	// win the slot before the stop is still in force.
	const hold = "trap '' TERM; printf 'ready\\n'; while [ ! -f finish ]; do sleep 0.02; done"
	testutil.NoError(t, d.SetBackend("draining", config.Backend{Command: hold}))
	task := &model.Task{Name: "Draining", Backend: "draining", Worktree: t.TempDir(), Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	initial, err := run.Start(task, d.Config(), 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(task.Worktree, "finish"), nil, 0600) })
	deadline := time.Now().Add(6 * time.Second)
	for !initial.IsIdle() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	testutil.True(t, initial.IsIdle())

	stop := httptest.NewRecorder()
	s.routes().ServeHTTP(stop, authedReq(http.MethodPost, "/api/tasks/"+task.ID+"/stop", ""))
	testutil.Equal(t, stop.Code, http.StatusOK)
	testutil.True(t, initial.Alive())
	resume := httptest.NewRecorder()
	s.routes().ServeHTTP(resume, authedReq(http.MethodPost, "/api/tasks/"+task.ID+"/resume", ""))
	testutil.Equal(t, resume.Code, http.StatusConflict)
	testutil.Contains(t, resume.Body.String(), "agent_restarting")
	voiceErr := s.VoiceOperator().Resume(context.Background(), task.ID, false)
	var opErr *voice.OpError
	if !errors.As(voiceErr, &opErr) {
		t.Fatalf("expected typed voice conflict, got %v", voiceErr)
	}
	testutil.Equal(t, opErr.Code, "agent_restarting")
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInReview)
}

type legacyVoiceStopRunner struct{ *agent.Runner }

func (legacyVoiceStopRunner) StopSettled(string) error {
	return agent.ErrSessionCoordinationUnsupported
}

func TestVoiceStopExplainsLegacySupervisor(t *testing.T) {
	s, d := testServer(t)
	s.runner = legacyVoiceStopRunner{agent.NewRunner(nil)}
	task := &model.Task{Name: "Review", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))

	err := s.VoiceOperator().Stop(context.Background(), task.ID)
	var opErr *voice.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected typed voice refusal, got %v", err)
	}
	testutil.Equal(t, opErr.Code, "supervisor_restart_required")
	testutil.Contains(t, opErr.Speech, "restart the session supervisor")
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
}

func TestRESTStopFencesResidentRestartWithoutChangingStatus(t *testing.T) {
	s, d := testServer(t)
	resident, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)

	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/"+resident.ID+"/stop", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	row, err := d.Get(resident.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
	generation, err := d.SessionStopGeneration(resident.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, generation, int64(1))
}

func TestRESTStopRefusesQueuedRestart(t *testing.T) {
	s, d := testServer(t)
	exiting, allowRestart := make(chan struct{}), make(chan struct{})
	var firstExit, release sync.Once
	unblock := func() { release.Do(func() { close(allowRestart) }) }
	run := agent.NewRunner(func(string, error, bool, []byte) {
		firstExit.Do(func() {
			close(exiting)
			<-allowRestart
		})
	})
	s.runner = run
	t.Cleanup(run.StopAll)
	t.Cleanup(unblock)
	testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "sleep 60"}))
	task := &model.Task{Name: "Review", Backend: "plain", Worktree: t.TempDir(), Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	initial, err := run.Start(task, d.Config(), 24, 80, false)
	testutil.NoError(t, err)
	testutil.NoError(t, run.Revive(task, d.Config(), 24, 80))
	select {
	case <-exiting:
	case <-time.After(5 * time.Second):
		t.Fatal("outgoing session did not exit")
	}
	testutil.True(t, run.HasPendingRestart(task.ID))

	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq(http.MethodPost, "/api/tasks/"+task.ID+"/stop", ""))
	unblock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sess := run.Get(task.ID)
		if sess != nil && sess.Alive() && sess.PID() != initial.PID() {
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Contains(t, w.Body.String(), "agent_restarting")
			row, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, row.Status, model.StatusInProgress)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("replacement session did not start")
}

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
