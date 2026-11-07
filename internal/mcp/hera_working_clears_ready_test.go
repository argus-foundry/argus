package mcp

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type failClearReadyStore struct{ HeraStore }

func (failClearReadyStore) ClearHeraReadyToClose(string) error { return errors.New("boom") }

func readyMarker(t *testing.T, d *db.DB, taskID string) bool {
	t.Helper()
	meta, err := d.ListMeta(taskID, db.HeraMetaNamespace)
	testutil.NoError(t, err)
	for _, e := range meta {
		if e.Key == db.HeraMetaKeyReadyToClose && e.Value == "true" {
			return true
		}
	}
	return false
}

func roleStatusOf(t *testing.T, d *db.DB, orch, role string) db.HeraRoleStatusValue {
	t.Helper()
	o, err := d.HeraOrchestratorByName(orch)
	testutil.NoError(t, err)
	r, err := d.HeraRoleByName(o.ID, role)
	testutil.NoError(t, err)
	rs, err := d.HeraRoleStatusFor(r.ID)
	testutil.NoError(t, err)
	return rs.Status
}

func TestWorkerWorking_ClearsReadyToClose(t *testing.T) {
	setStatus := map[string]func(t *testing.T, s *Server, cwd, status string){
		"hera_status": func(t *testing.T, s *Server, cwd, status string) {
			testutil.Equal(t, heraStatus(t, s, cwd, status).IsError, false)
		},
		"hera_send": func(t *testing.T, s *Server, cwd, status string) {
			testutil.Equal(t, heraSend(t, s, cwd, "b", "t", status, "").IsError, false)
		},
	}
	for name, set := range setStatus {
		t.Run(name, func(t *testing.T) {
			s, d := testHeraServer(t)
			seedCoordinator(t, s, d, "myorch", "/wt/coord")
			worker := addHeraTestTask(t, d, "/wt/worker")
			attachWorker(t, s, "myorch", worker.Worktree)

			set(t, s, worker.Worktree, "done")
			testutil.Equal(t, readyMarker(t, d, worker.ID), true)

			set(t, s, worker.Worktree, "working")
			testutil.Equal(t, readyMarker(t, d, worker.ID), false)
			testutil.Equal(t, roleStatusOf(t, d, "myorch", "w1"), db.HeraStatusWorking)
			got, _ := d.Get(worker.ID)
			testutil.Equal(t, got.Status, model.StatusInReview)

			// done again: task is already in_review, so existing roll rules do not re-stamp.
			set(t, s, worker.Worktree, "done")
			testutil.Equal(t, readyMarker(t, d, worker.ID), false)

			// Once the task is back in_progress, done re-establishes readiness.
			testutil.NoError(t, d.SetStatus(worker.ID, model.StatusInProgress))
			set(t, s, worker.Worktree, "working")
			set(t, s, worker.Worktree, "done")
			testutil.Equal(t, readyMarker(t, d, worker.ID), true)
		})
	}
}

func TestWorkerWorking_AcceptedTaskStaysComplete(t *testing.T) {
	s, d := testHeraServer(t)
	seedCoordinator(t, s, d, "myorch", "/wt/coord")
	worker := addHeraTestTask(t, d, "/wt/worker")
	attachWorker(t, s, "myorch", worker.Worktree)
	heraStatus(t, s, worker.Worktree, "done")
	testutil.NoError(t, d.SetStatus(worker.ID, model.StatusComplete))

	testutil.Equal(t, heraStatus(t, s, worker.Worktree, "working").IsError, false)
	got, _ := d.Get(worker.ID)
	testutil.Equal(t, got.Status, model.StatusComplete)
	testutil.Equal(t, readyMarker(t, d, worker.ID), false)
}

func TestWorking_DoesNotClearOtherRolesMarkers(t *testing.T) {
	s, d := testHeraServer(t)
	coord := seedCoordinator(t, s, d, "myorch", "/wt/coord")
	w1 := addHeraTestTask(t, d, "/wt/w1")
	attachWorker(t, s, "myorch", w1.Worktree)
	w2 := addHeraTestTask(t, d, "/wt/w2")
	testutil.Equal(t, callResult(t, doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "hera_join",
		Arguments: []byte(`{"cwd":"/wt/w2","orchestrator":"myorch","role_name":"w2","kind":"worker"}`),
	})).IsError, false)

	heraStatus(t, s, w1.Worktree, "done")
	heraStatus(t, s, w2.Worktree, "done")
	testutil.NoError(t, d.SetMeta(coord.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))

	heraStatus(t, s, w2.Worktree, "working")
	testutil.Equal(t, readyMarker(t, d, w2.ID), false)
	testutil.Equal(t, readyMarker(t, d, w1.ID), true)

	heraStatus(t, s, coord.Worktree, "working")
	testutil.Equal(t, readyMarker(t, d, w1.ID), true)
	testutil.Equal(t, readyMarker(t, d, coord.ID), true)
}

func TestWorkerWorking_ClearFailureKeepsStatus(t *testing.T) {
	s, d := testHeraServer(t)
	seedCoordinator(t, s, d, "myorch", "/wt/coord")
	worker := addHeraTestTask(t, d, "/wt/worker")
	attachWorker(t, s, "myorch", worker.Worktree)
	heraStatus(t, s, worker.Worktree, "done")

	s.heraStore = failClearReadyStore{s.heraStore}

	cr := heraStatus(t, s, worker.Worktree, "working")
	testutil.Equal(t, cr.IsError, true)
	testutil.Contains(t, cr.Content[0].Text, "clear readiness failed")
	testutil.Equal(t, roleStatusOf(t, d, "myorch", "w1"), db.HeraStatusDone)
	testutil.Equal(t, readyMarker(t, d, worker.ID), true)

	// hera_send soft-fails the status apply but still delivers the message.
	cr = heraSend(t, s, worker.Worktree, "b", "t", "working", "")
	testutil.Equal(t, cr.IsError, false)
	testutil.Contains(t, cr.Content[0].Text, "Message sent")
	testutil.Equal(t, roleStatusOf(t, d, "myorch", "w1"), db.HeraStatusDone)
	testutil.Equal(t, readyMarker(t, d, worker.ID), true)
}

func TestWorking_NonWorkerKeepsOwnReadiness(t *testing.T) {
	for _, kind := range []db.HeraRoleKind{db.HeraKindFreelance, db.HeraKindCoordinator} {
		for _, tool := range []string{"hera_status", "hera_send"} {
			t.Run(string(kind)+"/"+tool, func(t *testing.T) {
				s, d := testHeraServer(t)
				seedCoordinator(t, s, d, "myorch", "/wt/coord")
				task := addHeraTestTask(t, d, "/wt/nonworker")
				orch, err := d.HeraOrchestratorByName("myorch")
				testutil.NoError(t, err)
				_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "nonworker", Kind: kind}, task.ID, task.Worktree)
				testutil.NoError(t, err)
				testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				if tool == "hera_status" {
					testutil.Equal(t, heraStatus(t, s, task.Worktree, "working").IsError, false)
				} else {
					testutil.Equal(t, heraSend(t, s, task.Worktree, "b", "t", "working", "coord").IsError, false)
				}
				testutil.Equal(t, readyMarker(t, d, task.ID), true)
				got, err := d.Get(task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, got.Status, task.Status)
			})
		}
	}
}
