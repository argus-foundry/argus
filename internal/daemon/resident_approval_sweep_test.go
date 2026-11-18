package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentSweepApprovalsLapsesOldLaunch(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	task, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	testutil.NotNil(t, task)
	_, err = e.db.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	call := db.ApprovalCall{TaskID: task.ID, Launch: "launch-1", Session: "s", Cwd: task.Worktree, Tool: "Bash", Input: json.RawMessage(`{"command":"echo probe"}`)}
	a, _, err := e.db.RequestResidentApproval(call, e.now)
	testutil.NoError(t, err)

	launch := "launch-1"
	e.m.approvalLaunch = func() string { return launch }
	e.m.sweepApprovals()
	open, err := e.db.ListHeraBoardItems(a.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(open), 1)

	launch = "launch-2" // the resident was relaunched
	e.m.sweepApprovals()
	open, err = e.db.ListHeraBoardItems(a.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(open), 0)
	all, err := e.db.ListHeraBoardItems(a.RoleID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, all[len(all)-1].ApprovalState, db.ApprovalStale)
	notices, err := e.db.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
	testutil.Contains(t, notices[0].Body, "relaunched before it was used")

	e.now = e.now.Add(time.Hour)
	e.m.sweepApprovals() // idempotent
	notices, err = e.db.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
}

func TestResidentPostBoardRefreshesRepeatedNotice(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	task, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	e.m.postBoard(task, "fyi", "Solo moved accounts: default to personal", "first")
	e.m.postBoard(task, "fyi", "Solo moved accounts: default to personal", "second")
	orch, err := e.db.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	roles, err := e.db.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	items, err := e.db.ListHeraBoardItems(roles[0].ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Body, "second")
}
