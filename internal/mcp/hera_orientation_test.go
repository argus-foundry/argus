package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

// keep-coordinator-orientation-on-recycle D3/D5.

func callNewOrchestrator(t *testing.T, s *Server, worktree, extra string) string {
	t.Helper()
	resp := doRequest(t, s, "tools/call", ToolCallParams{
		Name: "hera_new_orchestrator",
		Arguments: json.RawMessage(fmt.Sprintf(`{
			"cwd": %q, "name": "child", "coordinator_role_name": "coord"%s
		}`, worktree, extra)),
	})
	testutil.NoError(t, respErr(resp))
	cr := callResult(t, resp)
	testutil.Equal(t, cr.IsError, false)
	return cr.Content[0].Text
}

func TestHera_NewOrchestrator_PlainTaskGetsFullOrientation(t *testing.T) {
	s, d := testHeraServer(t)
	task := addHeraTestTask(t, d, "/wt/plain")
	text := callNewOrchestrator(t, s, task.Worktree, "")
	testutil.Contains(t, text, "## Coordinator orientation")
	testutil.Contains(t, text, agent.HeraCoordinatorOrientation("child"))
}

func TestHera_NewOrchestrator_PromotedWorkerGetsSubCoordOrientation(t *testing.T) {
	s, d := testHeraServer(t)
	parent, err := d.CreateHeraOrchestrator("parent", "")
	testutil.NoError(t, err)
	task := addHeraTestTask(t, d, "/wt/worker")
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: parent.ID, Name: "w1", Kind: db.HeraKindWorker, ArgusProject: "test-project",
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)

	text := callNewOrchestrator(t, s, task.Worktree, "")
	testutil.Contains(t, text, agent.HeraSubCoordinatorOrientation("child", "parent", "coord"))
	if strings.Contains(text, "five habits") {
		t.Fatal("promoted worker must not get the top-level orientation")
	}
}

func coordRolePrompt(t *testing.T, d *db.DB) string {
	t.Helper()
	orch, err := d.HeraOrchestratorByName("child")
	testutil.NoError(t, err)
	roles, err := d.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	testutil.Equal(t, len(roles), 1)
	return roles[0].Prompt
}

func TestHera_NewOrchestrator_RolePromptFallsBackToTaskPrompt(t *testing.T) {
	t.Run("omitted prompt uses task prompt", func(t *testing.T) {
		s, d := testHeraServer(t)
		task := addHeraTestTask(t, d, "/wt/fallback")
		task.Prompt = "build the importer"
		testutil.NoError(t, d.Update(task))
		callNewOrchestrator(t, s, task.Worktree, "")
		testutil.Equal(t, coordRolePrompt(t, d), "build the importer")
	})
	t.Run("explicit prompt wins", func(t *testing.T) {
		s, d := testHeraServer(t)
		task := addHeraTestTask(t, d, "/wt/explicit")
		task.Prompt = "build the importer"
		testutil.NoError(t, d.Update(task))
		callNewOrchestrator(t, s, task.Worktree, `, "prompt": "own the importer sub-team"`)
		testutil.Equal(t, coordRolePrompt(t, d), "own the importer sub-team")
	})
}
