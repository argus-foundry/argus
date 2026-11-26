package tui

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type launchTestRunner struct {
	*agent.Runner
	start func(*model.Task, bool) (agent.SessionHandle, error)
}

func (r launchTestRunner) Start(task *model.Task, _ config.Config, _, _ uint16, resume bool) (agent.SessionHandle, error) {
	return r.start(task, resume)
}

func TestStartSessionPreservesStopDuringRunnerReply(t *testing.T) {
	for _, tc := range []struct {
		name     string
		startErr error
	}{
		{name: "success"},
		{name: "failure", startErr: errors.New("runner failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{
				ID: "review", Name: "Review", SessionID: "previous-session",
				Status: model.StatusInReview,
			}
			testutil.NoError(t, d.Add(task))
			runner := launchTestRunner{Runner: agent.NewRunner(nil), start: func(_ *model.Task, resume bool) (agent.SessionHandle, error) {
				testutil.True(t, resume)
				// The stop is committed before Start's response is processed.
				// The status was already InReview, so only its generation changes.
				testutil.NoError(t, d.RecordSessionStop(task.ID))
				return &moveTestSession{idle: true}, tc.startErr
			}}
			app := New(d, runner, false)

			app.startSession(task)

			stored, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.Status, model.StatusInReview)
			testutil.Equal(t, stored.SessionID, "previous-session")
			testutil.Equal(t, stored.AgentPID, 0)
			_, markedStarted := app.recentStarts[task.ID]
			testutil.False(t, markedStarted)
		})
	}
}

func TestStartSessionAmbiguousPreservesMintedSessionID(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "fresh", Name: "Fresh", Project: "p"}
	testutil.NoError(t, d.Add(task))
	var requestedID string
	runner := launchTestRunner{Runner: agent.NewRunner(nil), start: func(task *model.Task, resume bool) (agent.SessionHandle, error) {
		requestedID = task.SessionID
		testutil.False(t, resume)
		return nil, agent.ErrStartAmbiguous
	}}
	app := New(d, runner, false)

	app.startSession(task)

	stored, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, requestedID == "")
	testutil.Equal(t, stored.SessionID, requestedID)
	testutil.Equal(t, stored.Status, model.StatusPending)
	testutil.Equal(t, app.statusbar.Info(), "Session start unconfirmed")
}

type remoteLaunchStore struct {
	creatorStore
	updates int
}

func (s *remoteLaunchStore) Update(*model.Task) error {
	s.updates++
	return nil
}

func TestRemoteStartSessionLeavesPersistenceToServer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		startErr error
	}{
		{name: "success"},
		{name: "failure", startErr: errors.New("request failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &remoteLaunchStore{}
			runner := launchTestRunner{Runner: agent.NewRunner(nil), start: func(task *model.Task, _ bool) (agent.SessionHandle, error) {
				testutil.Equal(t, task.SessionID, "")
				return &moveTestSession{idle: true}, tc.startErr
			}}
			app := New(s, runner, false)

			app.startSession(&model.Task{ID: "remote", Name: "Remote", Status: model.StatusInReview})

			testutil.Equal(t, s.updates, 0)
		})
	}
}
