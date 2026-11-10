package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHera_JoinMove_RefusedForPendingGraduate(t *testing.T) {
	s, d := testHeraServer(t)
	coord := addHeraTestTask(t, d, "/wt/grad-coord")
	doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "hera_new_orchestrator",
		Arguments: json.RawMessage(fmt.Sprintf(`{"cwd":%q,"name":"grad-orch","coordinator_role_name":"coord"}`, coord.Worktree)),
	})
	grad := addHeraTestTask(t, d, "/wt/grad-pending")
	testutil.NoError(t, d.SetMeta(grad.ID, db.HeraMetaNamespace, db.HeraMetaKeyGraduationPendingRestart, "true"))

	call := func(name, args string) ToolCallResult {
		return callResult(t, doRequest(t, s, "tools/call", ToolCallParams{Name: name, Arguments: json.RawMessage(args)}))
	}
	attach := fmt.Sprintf(`{"cwd":%q,"orchestrator":"grad-orch","role_name":"w1","kind":"worker"}`, grad.Worktree)
	move := fmt.Sprintf(`{"cwd":%q,"orchestrator":"grad-orch","role_name":"w1","kind":"worker"}`, grad.Worktree)
	claim := fmt.Sprintf(`{"cwd":%q}`, grad.Worktree)

	for _, tc := range []struct{ name, tool, args string }{
		{"join attach", "hera_join", attach},
		{"join claim", "hera_join", claim},
		{"move", "hera_move", move},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := call(tc.tool, tc.args)
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, "graduation restart is still pending")
			live, err := d.ListHeraLiveBindingsByTask(grad.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(live), 0)
		})
	}

	// Once the state clears, attach works normally again.
	testutil.NoError(t, d.ClearGraduationState(grad.ID))
	cr := call("hera_join", attach)
	testutil.False(t, cr.IsError)
	live, err := d.ListHeraLiveBindingsByTask(grad.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(live), 1)
}

// gradCheckFailDB fails the graduation-state read the way a broken DB would.
type gradCheckFailDB struct{ *db.DB }

func (gradCheckFailDB) GraduationPendingErr(string) (bool, error) {
	return false, errors.New("disk on fire")
}

// An unreadable graduation state fails closed: membership changes are refused
// rather than risking a move of a graduate whose restart is still tracked.
func TestHera_JoinMove_RefusedWhenGraduationStateUnreadable(t *testing.T) {
	s, d := testHeraServer(t)
	coord := addHeraTestTask(t, d, "/wt/gradfail-coord")
	doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "hera_new_orchestrator",
		Arguments: json.RawMessage(fmt.Sprintf(`{"cwd":%q,"name":"gradfail-orch","coordinator_role_name":"coord"}`, coord.Worktree)),
	})
	task := addHeraTestTask(t, d, "/wt/gradfail-task")
	s.SetTaskManager(s.createTask, gradCheckFailDB{d}, s.taskStopper)

	args := fmt.Sprintf(`{"cwd":%q,"orchestrator":"gradfail-orch","role_name":"w1","kind":"worker"}`, task.Worktree)
	for _, tool := range []string{"hera_join", "hera_move"} {
		t.Run(tool, func(t *testing.T) {
			cr := callResult(t, doRequest(t, s, "tools/call", ToolCallParams{Name: tool, Arguments: json.RawMessage(args)}))
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, tool+" refused: could not verify graduation state")
			testutil.Contains(t, cr.Content[0].Text, "disk on fire")
			live, err := d.ListHeraLiveBindingsByTask(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(live), 0)
		})
	}
}
