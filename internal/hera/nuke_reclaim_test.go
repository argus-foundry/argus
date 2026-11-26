package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type nukeReclaimSession struct {
	agent.SessionHandle
}

func (nukeReclaimSession) PID() int { return 42 }

type nukeReclaimOwner struct {
	*agent.Runner
	stop    func(string, int) error
	pending *bool
}

func (r nukeReclaimOwner) AttachableSession(string) (agent.SessionHandle, error) {
	if *r.pending {
		return nil, agent.ErrSessionStarting
	}
	return nukeReclaimSession{}, nil
}

func (r nukeReclaimOwner) StopTreeForPID(taskID string, pid int) error {
	return r.stop(taskID, pid)
}

func (r nukeReclaimOwner) CheckReclaimSessionRetired(string) error {
	return agent.ErrSessionStarting
}

func TestNukeRoleArmsScopedStopForReviewTask(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	task := &model.Task{ID: "worker", Name: "Worker", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	orch, err := d.CreateHeraOrchestrator("old-team", "")
	testutil.NoError(t, err)
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orch.ID, Name: "original", Kind: db.HeraKindWorker,
	}, task.ID, "")
	testutil.NoError(t, err)
	next, err := d.CreateHeraOrchestrator("other-team", "")
	testutil.NoError(t, err)

	called := false
	pending := true
	owner := nukeReclaimOwner{
		Runner:  agent.NewRunner(nil),
		pending: &pending,
		stop: func(id string, pid int) error {
			called = true
			testutil.Equal(t, id, task.ID)
			testutil.Equal(t, pid, 42)
			armed, err := d.HeraReclaimStopPID(id)
			testutil.NoError(t, err)
			testutil.Equal(t, armed, pid)
			_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
				OrchestratorID: next.ID, Name: "joining", Kind: db.HeraKindWorker,
			}, task.ID, "")
			testutil.ErrorIs(t, err, db.ErrHeraReclaimInProgress)
			return nil
		},
	}

	unlock, acquired, err := d.TryLockSessionMutation(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	_, err = NukeSingleRole(d, owner, &heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true})
	testutil.NoError(t, err)
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, row.Archived)
	unlock()
	RetryNukeStop(d, owner, task.ID)
	testutil.False(t, called)
	pending = false
	RetryNukeStop(d, owner, task.ID)
	testutil.True(t, called)
	row, err = d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, row.Archived)
	testutil.Equal(t, row.Status, model.StatusComplete)
	armed, err := d.HeraReclaimStopPID(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, armed, 42)
}
