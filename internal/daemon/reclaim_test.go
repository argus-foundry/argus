package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type reclaimProbe struct {
	*fakeSupClient
	info       SessionInfo
	retirement error
	exit       ExitInfo
}

func (o reclaimProbe) ProbeSession(string) (SessionInfo, error) { return o.info, nil }
func (o reclaimProbe) CheckReclaimSessionRetired(string) error  { return o.retirement }
func (o reclaimProbe) ObservedSessionExit(string, int) ExitInfo { return o.exit }

func TestRecoverHeraReclaimExitValidatesOwner(t *testing.T) {
	const pid = 41
	for _, tc := range []struct {
		name       string
		info       SessionInfo
		retirement error
		exit       ExitInfo
		committed  bool
		recovered  bool
	}{
		{name: "owner unsettled", retirement: agent.ErrSessionStarting, exit: ExitInfo{PID: pid, Stopped: true}},
		{name: "replacement became live", info: SessionInfo{Alive: true, PID: pid + 1}, exit: ExitInfo{PID: pid, Stopped: true}},
		{name: "replacement already retired", info: SessionInfo{LastExitPID: pid + 1}, exit: ExitInfo{PID: pid, Stopped: true}},
		{name: "unconfirmed relay", exit: ExitInfo{PID: pid, StreamLost: true}},
		{name: "different receipt", exit: ExitInfo{PID: pid + 1, Stopped: true}},
		{name: "pending restart receipt", exit: ExitInfo{PID: pid, PendingRestart: true}},
		{name: "matching receipt", exit: ExitInfo{PID: pid, Stopped: true}, recovered: true},
		{name: "status already committed", exit: ExitInfo{PID: pid, Stopped: true}, committed: true, recovered: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := testDaemon(t)
			database := d.db
			task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress, Archived: true}
			testutil.NoError(t, database.Add(task))
			armed, err := database.ArmHeraReclaimStop(task.ID, pid)
			testutil.NoError(t, err)
			testutil.True(t, armed)
			if tc.committed {
				testutil.NoError(t, database.SetStatus(task.ID, model.StatusComplete))
			}
			d.UseSupervisorRunner(reclaimProbe{fakeSupClient: &fakeSupClient{}, info: tc.info, retirement: tc.retirement, exit: tc.exit})

			recovered := d.recoverHeraReclaimExits()
			pending, err := database.HeraReclaimStopPID(task.ID)
			testutil.NoError(t, err)
			row, err := database.Get(task.ID)
			testutil.NoError(t, err)
			if tc.recovered {
				testutil.Equal(t, recovered, 1)
				testutil.Equal(t, pending, 0)
				testutil.Equal(t, row.Status, model.StatusComplete)
				testutil.Equal(t, d.recoverHeraReclaimExits(), 0)
			} else {
				testutil.Equal(t, recovered, 0)
				testutil.Equal(t, pending, pid)
				testutil.Equal(t, row.Status, model.StatusInProgress)
			}
		})
	}
}
