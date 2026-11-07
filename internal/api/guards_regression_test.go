package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/app/agentview"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type guardRunner struct {
	agent.SessionRunner
	startErr error
	get      func() agent.SessionHandle
	calls    int
}

func (r *guardRunner) StartOrReattach(task *model.Task, cfg config.Config, rows, cols uint16, resume bool) (agent.SessionHandle, bool, error) {
	r.calls++
	if r.startErr == nil {
		return r.SessionRunner.StartOrReattach(task, cfg, rows, cols, resume)
	}
	return nil, false, r.startErr
}
func (r *guardRunner) Get(id string) agent.SessionHandle {
	if r.get != nil {
		return r.get()
	}
	return r.SessionRunner.Get(id)
}

func TestReviveLaunchErrors(t *testing.T) {
	for _, ep := range []string{"resume", "restart"} {
		for _, st := range []db.HeraRoleStatusValue{db.HeraStatusDone, db.HeraStatusFailed} {
			for _, ambiguous := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/ambiguous=%t", ep, st, ambiguous), func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					s, d := testServer(t)
					task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
					testutil.NoError(t, d.UpsertHeraRoleStatus(role, st))
					before, err := d.HeraRoleStatusFor(role)
					testutil.NoError(t, err)
					launchErr := errors.New("definite launch failure")
					if ambiguous {
						launchErr = fmt.Errorf("supervisor timeout: %w", agent.ErrStartAmbiguous)
					}
					runner := &guardRunner{SessionRunner: s.runner, startErr: launchErr}
					s.runner = runner
					w := doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, `{"force_revive":true}`, nil)
					code, status := "start_failed", http.StatusInternalServerError
					if ambiguous {
						code, status = "start_ambiguous", http.StatusServiceUnavailable
					}
					testutil.Equal(t, w.Code, status)
					testutil.Equal(t, decodeBody(t, w)["code"], any(code))
					after, err := d.HeraRoleStatusFor(role)
					testutil.NoError(t, err)
					if ambiguous {
						testutil.Equal(t, after.Status, db.HeraStatusWorking)
						testutil.Equal(t, readyMeta(t, d, task.ID), "false")
					} else {
						testutil.DeepEqual(t, after, before)
						testutil.Equal(t, readyMeta(t, d, task.ID), "")
					}
					testutil.Equal(t, runner.calls, 1)
				})
			}
		}
	}
}

func TestRevivePendingPrecedence(t *testing.T) {
	for _, ep := range []string{"resume", "restart"} {
		for _, st := range []model.Status{model.StatusPending, model.StatusInProgress, model.StatusInReview, model.StatusComplete} {
			for _, force := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/force=%t", ep, st, force), func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					s, d := testServer(t)
					task, role := mkHeraTask(t, d, db.HeraKindWorker, st)
					testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
					testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
					s.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, true)
					runner := &guardRunner{SessionRunner: s.runner, startErr: errors.New("unexpected launch")}
					s.runner = runner
					w := doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, fmt.Sprintf(`{"force_revive":%t}`, force), nil)
					testutil.Equal(t, w.Code, http.StatusConflict)
					testutil.Equal(t, decodeBody(t, w)["code"], any("agent_restarting"))
					testutil.Equal(t, readyMeta(t, d, task.ID), "true")
					rs, err := d.HeraRoleStatusFor(role)
					testutil.NoError(t, err)
					testutil.Equal(t, rs.Status, db.HeraStatusDone)
					got, err := d.Get(task.ID)
					testutil.NoError(t, err)
					testutil.Equal(t, got.Status, st)
					testutil.Equal(t, runner.calls, 0)
				})
			}
		}
	}
}

func TestParseForceReviveStrict(t *testing.T) {
	for _, body := range []string{`{"force_revive":true}` + strings.Repeat(" ", 1024) + "junk", strings.Repeat(" ", 1025), `{"force_revive":true} {}`, `null`, `[]`, `true`} {
		t.Run(fmt.Sprintf("%.25s/%d", body, len(body)), func(t *testing.T) {
			w := httptest.NewRecorder()
			_, ok := parseForceRevive(w, authedReq("POST", "/", body))
			testutil.False(t, ok)
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Equal(t, decodeBody(t, w)["code"], any("invalid_body"))
		})
	}
	for _, body := range []string{"", " ", `{}`, `{"force_revive":false}`, `{"force_revive":true}`} {
		t.Run("valid/"+body, func(t *testing.T) {
			w := httptest.NewRecorder()
			force, ok := parseForceRevive(w, authedReq("POST", "/", body))
			testutil.True(t, ok)
			testutil.Equal(t, force, strings.Contains(body, "true"))
		})
	}
}

type stalledGuardBody struct{ entered, release chan struct{} }

func (b *stalledGuardBody) Read([]byte) (int, error) { close(b.entered); <-b.release; return 0, io.EOF }
func (*stalledGuardBody) Close() error               { return nil }

func TestGuardBodyDoesNotBlockArchive(t *testing.T) {
	for _, ep := range []string{"resume", "restart", "input"} {
		t.Run(ep, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, d := testServer(t)
			testutil.NoError(t, s.Shutdown(context.Background()))
			task := &model.Task{Name: "slow", Status: model.StatusInReview}
			testutil.NoError(t, d.Add(task))
			sess := &guardInputSession{}
			s.runner = &guardRunner{SessionRunner: s.runner, get: func() agent.SessionHandle { return sess }, startErr: errors.New("no launch")}
			body := &stalledGuardBody{make(chan struct{}), make(chan struct{})}
			req := authedReq("POST", "/api/tasks/"+task.ID+"/"+ep, "")
			req.Body = body
			done := make(chan struct{})
			go func() { s.routes().ServeHTTP(httptest.NewRecorder(), req); close(done) }()
			<-body.entered
			archiveDone := make(chan *httptest.ResponseRecorder, 1)
			go func() { archiveDone <- doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/archive", "", nil) }()
			select {
			case w := <-archiveDone:
				testutil.Equal(t, w.Code, http.StatusOK)
			case <-time.After(2 * time.Second):
				t.Error("stalled body blocked archive")
			}
			close(body.release)
			<-done
			if !t.Failed() {
				testutil.Equal(t, sess.writes.Load(), int64(0))
			}
		})
	}
}

type guardInputSession struct {
	agent.SessionHandle
	writes atomic.Int64
}

func (*guardInputSession) IsIdle() bool { return true }

func (s *guardInputSession) WriteInput(b []byte, _ agentview.InputOrigin) (int, error) {
	s.writes.Add(int64(len(b)))
	return len(b), nil
}

func TestInputArchiveLockDeterministic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, d := testServer(t)
	testutil.NoError(t, s.Shutdown(context.Background()))
	task := &model.Task{Name: "input", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	entered, release := make(chan struct{}), make(chan struct{})
	sess := &guardInputSession{}
	s.runner = &guardRunner{SessionRunner: s.runner, get: func() agent.SessionHandle { close(entered); <-release; return sess }}
	inputDone := make(chan struct{})
	go func() { doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/input", "x", nil); close(inputDone) }()
	<-entered
	archiveDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { archiveDone <- doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/archive", "", nil) }()
	// Wait until archive is queued on the lock or has overtaken the input.
	archived := false
	deadline := time.After(5 * time.Second)
wait:
	for {
		select {
		case w := <-archiveDone:
			testutil.Equal(t, w.Code, http.StatusOK)
			archived = true
			break wait
		case <-deadline:
			t.Error("archive never reached the task lock")
			break wait
		default:
		}
		s.taskLocks.mu.Lock()
		entry := s.taskLocks.m[task.ID]
		queued := entry != nil && entry.refs == 2
		s.taskLocks.mu.Unlock()
		if queued {
			break
		}
		runtime.Gosched()
	}
	testutil.False(t, archived)
	before := sess.writes.Load()
	close(release)
	<-inputDone
	if !archived {
		testutil.Equal(t, (<-archiveDone).Code, http.StatusOK)
	}
	testutil.Equal(t, sess.writes.Load(), before+1)
	if archived {
		testutil.Equal(t, sess.writes.Load(), before)
	}
	w := doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/input", "late", nil)
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Equal(t, sess.writes.Load(), int64(1))
}

func TestGuardSessionFilesIsolated(t *testing.T) {
	outer := t.TempDir()
	t.Setenv("HOME", outer)
	t.Run("session", func(t *testing.T) {
		s, d := testServer(t)
		task := &model.Task{Name: "files", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		startCat(t, s, d, task)
		testutil.True(t, os.Getenv("HOME") != outer)
		for _, path := range []string{agent.SessionLogPath(task.ID), agent.SessionSizePath(task.ID)} {
			testutil.True(t, strings.HasPrefix(path, os.Getenv("HOME")+string(os.PathSeparator)))
			_, err := os.Stat(path)
			testutil.NoError(t, err)
		}
	})
	entries, err := os.ReadDir(outer)
	testutil.NoError(t, err)
	testutil.Equal(t, len(entries), 0)
}

func TestOrdinaryInProgressCloseout(t *testing.T) {
	for _, ep := range []string{"resume", "restart"} {
		t.Run(ep, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, d := testServer(t)
			task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInProgress)
			testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
			w := doReq(s.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, "", nil)
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Equal(t, decodeBody(t, w)["code"], any("hera_closeout"))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Status, model.StatusInProgress)
			testutil.Nil(t, s.runner.Get(task.ID))
		})
	}
}

func idleGuardSessionRunner(r agent.SessionRunner, sess agent.SessionHandle) agent.SessionRunner {
	return &guardRunner{SessionRunner: r, get: func() agent.SessionHandle { return &moveTestSession{SessionHandle: sess, idle: true} }}
}
