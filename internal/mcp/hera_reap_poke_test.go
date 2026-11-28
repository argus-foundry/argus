package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

// add-coord-reap-triggers: hera_status(done) and hera_accept poke the reap
// nudge watcher; other statuses do not, and a nil poke is safe.
func TestHeraReapPoke_StatusDone(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   int
	}{
		{"done", 1},
		{"working", 0},
		{"blocked", 0},
	} {
		t.Run(tc.status, func(t *testing.T) {
			s, d := testHeraServer(t)
			_, workerWt := setupOrchWithWorker(t, s, d)
			pokes := 0
			s.SetHeraReapPoke(func() { pokes++ })
			resp := doRequest(t, s, "tools/call", ToolCallParams{
				Name:      "hera_status",
				Arguments: json.RawMessage(fmt.Sprintf(`{"cwd":%q,"status":%q}`, workerWt, tc.status)),
			})
			testutil.NoError(t, respErr(resp))
			testutil.Equal(t, callResult(t, resp).IsError, false)
			testutil.Equal(t, pokes, tc.want)
		})
	}
}

func TestHeraReapPoke_NilPokeIsSafe(t *testing.T) {
	s, d := testHeraServer(t)
	_, workerWt := setupOrchWithWorker(t, s, d)
	resp := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "hera_status",
		Arguments: json.RawMessage(fmt.Sprintf(`{"cwd":%q,"status":"done"}`, workerWt)),
	})
	testutil.NoError(t, respErr(resp))
	testutil.Equal(t, callResult(t, resp).IsError, false)
}

func TestHeraReapPoke_Accept(t *testing.T) {
	s, d := testHeraServer(t)
	coordTask := seedCoordinator(t, s, d, "O", "/wt/coord")
	orch, err := d.HeraOrchestratorByName("O")
	testutil.NoError(t, err)
	workerTask := addHeraTestTask(t, d, "/wt/worker-1")
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orch.ID, Name: "worker-1", Kind: db.HeraKindWorker, ArgusProject: "test-project",
	}, workerTask.ID, workerTask.Worktree)
	testutil.NoError(t, err)
	pokes := 0
	s.SetHeraReapPoke(func() { pokes++ })
	resp := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "hera_accept",
		Arguments: json.RawMessage(fmt.Sprintf(`{"cwd":%q,"role_name":"worker-1","orchestrator":"O"}`, coordTask.Worktree)),
	})
	testutil.NoError(t, respErr(resp))
	testutil.Equal(t, callResult(t, resp).IsError, false)
	testutil.Equal(t, pokes, 1)
}
