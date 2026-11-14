package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type runtimeKickSession struct {
	agent.SessionHandle
	pid int
}

func (s *runtimeKickSession) Alive() bool         { return true }
func (s *runtimeKickSession) IsIdle() bool        { return true }
func (s *runtimeKickSession) PTYSize() (int, int) { return 80, 24 }
func (s *runtimeKickSession) PID() int            { return s.pid }

type runtimeKickRunner struct {
	agent.SessionRunner
	live    agent.SessionHandle
	kick    func(*model.Task) error
	pending bool
	starts  int
}

func (r *runtimeKickRunner) Get(string) agent.SessionHandle { return r.live }
func (r *runtimeKickRunner) HasPendingRestart(string) bool  { return r.pending }
func (r *runtimeKickRunner) KickRerender(task *model.Task, _ config.Config, _, _ uint16) error {
	return r.kick(task)
}
func (r *runtimeKickRunner) StartOrReattach(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, bool, error) {
	r.starts++
	return nil, false, errors.New("unexpected fallback start")
}

func TestRuntimeKickUnknown(t *testing.T) {
	for _, outcome := range []string{"replacement live", "original live", "no session"} {
		t.Run(outcome, func(t *testing.T) {
			srv, d := testServer(t)
			task := addStopped(t, d, func(task *model.Task) { task.SandboxOverride = "enabled"; task.Sandboxed = true })
			_, rev := runtimeGet(t, srv, task.ID)
			runner := &runtimeKickRunner{SessionRunner: srv.runner, live: &runtimeKickSession{pid: 100}}
			var launched model.Task
			runner.kick = func(task *model.Task) error {
				launched = *task
				switch outcome {
				case "replacement live":
					runner.live = &runtimeKickSession{pid: 200}
				case "no session":
					runner.live = nil
				}
				return errors.New("kick RPC timed out")
			}
			srv.runner = runner
			sink := installAPISink(t)
			w := runtimePatch(srv, task.ID, `{"prompt":"replacement prompt","sandbox_override":"disabled","apply":"restart"}`, master(ifMatch(rev)))
			testutil.Equal(t, w.Code, http.StatusInternalServerError)
			body := decodeBody(t, w)
			testutil.Equal(t, body["code"], any("restart_unknown"))
			testutil.Equal(t, body["settings_applied"], any(true))
			testutil.Equal(t, body["rolled_back"], any(false))
			testutil.Equal(t, body["started"], any(false))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, runtimeOf(got), runtimeOf(&launched))
			testutil.Equal(t, got.SandboxOverride, "disabled")
			testutil.Equal(t, got.Sandboxed, launched.Sandboxed)
			testutil.Equal(t, runner.starts, 0)
			events := runtimeChangedEvents(sink)
			testutil.Equal(t, len(events), 1)
			if len(events) == 1 {
				testutil.Equal(t, events[0]["started"], any(false))
			}
		})
	}
}

func TestRuntimeKickPending(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	runner := &runtimeKickRunner{SessionRunner: srv.runner, live: &runtimeKickSession{pid: 100}}
	runner.kick = func(*model.Task) error { runner.pending = true; return nil }
	srv.runner = runner
	_, rev := runtimeGet(t, srv, task.ID)
	sink := installAPISink(t)
	synctest.Test(t, func(t *testing.T) {
		w := runtimePatch(srv, task.ID, `{"prompt":"replacement","apply":"restart"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["code"], any("restart_unknown"))
		testutil.Equal(t, body["started"], any(false))
		testutil.Equal(t, body["settings_applied"], any(true))
		testutil.Equal(t, runner.starts, 0)
		events := runtimeChangedEvents(sink)
		testutil.Equal(t, len(events), 1)
		if len(events) == 1 {
			testutil.Equal(t, events[0]["started"], any(false))
		}
	})
}

func TestRuntimeKickLaunchFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("uses a real PTY runner")
	}
	srv, d := testServer(t)
	task := &model.Task{Name: "async restart", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	old := startCat(t, srv, d, task)
	_, rev := runtimeGet(t, srv, task.ID)
	restore := agent.SetPrelaunchForTest(func(context.Context, *model.Task, config.Config) error {
		return errors.New("replacement prelaunch failed")
	})
	defer restore()
	sink := installAPISink(t)
	w := runtimePatch(srv, task.ID, `{"prompt":"replacement","apply":"restart","force":true}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusInternalServerError)
	body := decodeBody(t, w)
	testutil.Equal(t, body["code"], any("restart_failed"))
	testutil.Equal(t, body["started"], any(false))
	testutil.Equal(t, body["settings_applied"], any(true))
	testutil.False(t, old.Alive())
	testutil.False(t, srv.runner.HasPendingRestart(task.ID))
	testutil.Nil(t, srv.liveRuntimeSession(task.ID))
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Prompt, "replacement")
	events := runtimeChangedEvents(sink)
	testutil.Equal(t, len(events), 1)
	if len(events) == 1 {
		testutil.Equal(t, events[0]["started"], any(false))
	}
}

func (r *runtimeKickRunner) ProbeRestart(string) (bool, bool, error) {
	return r.live != nil && r.live.Alive(), r.pending, nil
}

func TestRuntimeKickSlowPrelaunch(t *testing.T) {
	if testing.Short() {
		t.Skip("uses a real PTY runner")
	}
	srv, d := testServer(t)
	task := &model.Task{Name: "slow restart", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	release, finished := make(chan struct{}), make(chan struct{})
	rejected := errors.New("injected rejection after delay")
	// The exit goroutine belongs to the real runner, outside the synctest
	// bubble. Its failure callback runs after it clears the pending restart.
	srv.runner = agent.NewRunner(func(_ string, err error, _ bool, _ []byte) {
		if errors.Is(err, rejected) {
			close(finished)
		}
	})
	startCat(t, srv, d, task)
	_, rev := runtimeGet(t, srv, task.ID)
	restore := agent.SetPrelaunchForTest(func(context.Context, *model.Task, config.Config) error {
		<-release
		return rejected
	})
	defer restore()
	synctest.Test(t, func(t *testing.T) {
		w := runtimePatch(srv, task.ID, `{"prompt":"slow candidate","apply":"restart","force":true}`, ifMatch(rev))
		testutil.True(t, srv.runner.HasPendingRestart(task.ID))
		close(release)
		<-finished
		testutil.False(t, srv.runner.HasPendingRestart(task.ID))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["code"], any("restart_unknown"))
		testutil.Equal(t, body["started"], any(false))
		testutil.Equal(t, body["settings_applied"], any(true))
	})
}

type runtimeUncheckedRunner struct{ agent.SessionRunner }

func TestRuntimeKickUncheckedProbe(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	runner := &runtimeKickRunner{SessionRunner: srv.runner, live: &runtimeKickSession{pid: 100}}
	runner.kick = func(*model.Task) error { runner.live = nil; return nil }
	srv.runner = runtimeUncheckedRunner{SessionRunner: runner}
	_, rev := runtimeGet(t, srv, task.ID)
	w := runtimePatch(srv, task.ID, `{"prompt":"replacement","apply":"restart"}`, ifMatch(rev))
	testutil.Equal(t, w.Code, http.StatusInternalServerError)
	testutil.Equal(t, decodeBody(t, w)["code"], any("restart_unknown"))
	testutil.Equal(t, runner.starts, 0)
}
