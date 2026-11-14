package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestMoveAccountReviveLaunchErrors(t *testing.T) {
	for _, status := range []db.HeraRoleStatusValue{db.HeraStatusDone, db.HeraStatusFailed} {
		for _, ambiguous := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ambiguous=%t", status, ambiguous), func(t *testing.T) {
				runner := &moveTestRunner{startErr: errors.New("definite launch failure")}
				if ambiguous {
					runner.startErr = fmt.Errorf("supervisor timeout: %w", agent.ErrStartAmbiguous)
				}
				f := newCodexAPIFixture(t, runner, true)
				task, role := mkHeraTask(t, f.d, db.HeraKindWorker, model.StatusInReview)
				task.Backend, task.Worktree, task.SessionID = f.task.Backend, f.task.Worktree, f.task.SessionID
				testutil.NoError(t, f.d.Update(task))
				testutil.NoError(t, f.d.UpsertHeraRoleStatus(role, status))
				testutil.NoError(t, f.d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				before, err := f.d.HeraRoleStatusFor(role)
				testutil.NoError(t, err)
				w := postMove(f.srv, task.ID, `{"account":"cx","force_revive":true}`)
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				after, err := f.d.HeraRoleStatusFor(role)
				testutil.NoError(t, err)
				if ambiguous {
					testutil.Equal(t, after.Status, db.HeraStatusWorking)
					testutil.Equal(t, readyMeta(t, f.d, task.ID), "false")
				} else {
					testutil.DeepEqual(t, after, before)
					testutil.Equal(t, readyMeta(t, f.d, task.ID), "true")
				}
				got, err := f.d.Get(task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, got.Account, "cx")
			})
		}
	}
}

func TestMoveAccountRestartOutcome(t *testing.T) {
	for _, tc := range []struct {
		name              string
		kickErr, probeErr error
		gone              bool
		code              string
		started           bool
	}{
		{name: "kick error retains candidate", kickErr: errors.New("RPC timeout"), code: "restart_unknown"},
		{name: "probe error retains candidate", probeErr: errors.New("probe unavailable"), code: "restart_unknown"},
		{name: "confirmed dead replacement", gone: true, code: "restart_failed"},
		{name: "confirmed live replacement", started: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &moveTestRunner{live: &moveTestSession{idle: true}, kickErr: tc.kickErr, probeErr: tc.probeErr, probeDead: tc.gone}
			f := newCodexAPIFixture(t, runner, true)
			sink := installAPISink(t)
			w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
			body := decodeBody(t, w)
			if tc.started {
				testutil.Equal(t, w.Code, http.StatusOK)
			} else {
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				testutil.Equal(t, body["code"], any(tc.code))
				testutil.Equal(t, body["settings_applied"], any(true))
				testutil.Equal(t, body["rolled_back"], any(false))
				testutil.Equal(t, body["moved"], any(true))
			}
			testutil.Equal(t, body["started"], any(tc.started))
			testutil.Nil(t, runner.started)
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "cx")
			evs := accountEvents(sink)
			testutil.Equal(t, len(evs), 1)
			if len(evs) == 1 {
				testutil.Equal(t, evs[0]["started"], any(tc.started))
			}
			testutil.Equal(t, runner.probeCalls, map[bool]int{true: 0, false: 1}[tc.kickErr != nil])
		})
	}
}

func TestMoveAccountResidentGate(t *testing.T) {
	mux, id := residentFixture(t)
	w := serve(mux, "POST", "/api/tasks/"+id+"/account", `{"account":"cx","force":true,"force_revive":true}`)
	testutil.Equal(t, w.Code, http.StatusConflict)
	testutil.Contains(t, w.Body.String(), "resident")
}

func TestMoveAccountPendingRestart(t *testing.T) {
	runner := &moveTestRunner{live: &moveTestSession{idle: true}, probePending: true}
	f := newCodexAPIFixture(t, runner, true)
	synctest.Test(t, func(t *testing.T) {
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["code"], any("restart_unknown"))
		testutil.Equal(t, body["started"], any(false))
		testutil.Equal(t, body["rolled_back"], any(false))
		got, err := f.d.Get(f.task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Account, "cx")
		testutil.Nil(t, runner.started)
		testutil.True(t, runner.probeCalls > 1)
	})
}
