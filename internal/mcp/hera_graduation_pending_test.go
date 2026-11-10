package mcp

import (
	"encoding/json"
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
