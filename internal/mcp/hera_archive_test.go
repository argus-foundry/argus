package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type archRunner struct{ stopped []string }

func (a *archRunner) IsAlive(string) bool         { return false }
func (a *archRunner) IsIdle(string) bool          { return true }
func (a *archRunner) BlockedOnPrompt(string) bool { return false }
func (a *archRunner) Stop(id string) error        { a.stopped = append(a.stopped, id); return nil }

func archCall(t *testing.T, s *Server, args string) (string, bool) {
	t.Helper()
	resp := doRequest(t, s, "tools/call", ToolCallParams{Name: "hera_archive", Arguments: json.RawMessage(args)})
	testutil.NoError(t, respErr(resp))
	cr := callResult(t, resp)
	return cr.Content[0].Text, cr.IsError
}

func archWorkerRole(t *testing.T, d *db.DB, orchName, name string, status model.Status) (*model.Task, *db.HeraRole) {
	t.Helper()
	orch, err := d.HeraOrchestratorByName(orchName)
	testutil.NoError(t, err)
	task := addHeraTestTask(t, d, "/wt/"+name)
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orch.ID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "test-project",
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(task.ID, status))
	return task, role
}

func TestHeraArchive_NonCoordinatorRejected(t *testing.T) {
	s, d := testHeraServer(t)
	orch, _ := d.CreateHeraOrchestrator("O", "")
	wt := addHeraTestTask(t, d, "/wt/w")
	_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "test-project"}, wt.ID, wt.Worktree)
	testutil.NoError(t, err)
	out, isErr := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"orchestrator":"O"}`, wt.Worktree))
	testutil.Equal(t, isErr, true)
	testutil.Contains(t, out, "only coordinators")
}

func TestHeraArchive_OwnRoleRejected(t *testing.T) {
	s, d := testHeraServer(t)
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	out, isErr := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"role_name":"coord","orchestrator":"O"}`, c.Worktree))
	testutil.Equal(t, isErr, true)
	testutil.Contains(t, out, "own role")
}

func TestHeraArchive_SingleAndSweep(t *testing.T) {
	s, d := testHeraServer(t)
	run := &archRunner{}
	s.SetHeraArchiver(run, func(task *model.Task) (bool, string) {
		if task.Worktree == "/wt/open-pr" {
			return false, "open PR"
		}
		return true, ""
	})
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	doneTask, _ := archWorkerRole(t, d, "O", "done1", model.StatusComplete)
	prTask, _ := archWorkerRole(t, d, "O", "open-pr", model.StatusComplete)
	wipTask, _ := archWorkerRole(t, d, "O", "wip", model.StatusInProgress)

	// dry run writes nothing
	out, isErr := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"orchestrator":"O","dry_run":true}`, c.Worktree))
	testutil.Equal(t, isErr, false)
	testutil.Contains(t, out, "**archivable (dry run)**: done1")
	testutil.Contains(t, out, "open-pr: git work not confirmed landed")
	got, _ := d.Get(doneTask.ID)
	testutil.Equal(t, got.Archived, false)
	dr, _ := d.HeraRoleByName(mustOrch(t, d, "O"), "done1")
	testutil.Equal(t, dr.ArchivedAt == nil, true)
	testutil.Equal(t, len(run.stopped), 0)

	// sweep archives only the done-done one and reports reasons
	out, isErr = archCall(t, s, fmt.Sprintf(`{"cwd":%q,"orchestrator":"O"}`, c.Worktree))
	testutil.Equal(t, isErr, false)
	testutil.Contains(t, out, "**archived**: done1")
	testutil.Contains(t, out, "open-pr: git work not confirmed landed: open PR")
	testutil.Contains(t, out, "wip: task status is in_progress")
	got, _ = d.Get(doneTask.ID)
	testutil.Equal(t, got.Archived, true)
	for _, id := range []string{prTask.ID, wipTask.ID} {
		g, _ := d.Get(id)
		testutil.Equal(t, g.Archived, false)
	}

	// an archived role is no longer addressable (same as every other role tool)
	out, isErr = archCall(t, s, fmt.Sprintf(`{"cwd":%q,"role_name":"done1","orchestrator":"O"}`, c.Worktree))
	testutil.Equal(t, isErr, true)
	testutil.Contains(t, out, "not found")
}

func TestHeraArchive_NoGitCheckRefuses(t *testing.T) {
	s, d := testHeraServer(t)
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	task, _ := archWorkerRole(t, d, "O", "w", model.StatusComplete)
	out, _ := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"role_name":"w","orchestrator":"O"}`, c.Worktree))
	testutil.Contains(t, out, "cannot be confirmed")
	g, _ := d.Get(task.ID)
	testutil.Equal(t, g.Archived, false)
}

func TestHeraArchive_SweepBudget(t *testing.T) {
	s, d := testHeraServer(t)
	old := heraArchiveSweepBudget
	heraArchiveSweepBudget = 30 * time.Millisecond
	t.Cleanup(func() { heraArchiveSweepBudget = old })
	// Each git check outlasts the whole budget, so only the first role is evaluated.
	s.SetHeraArchiver(&archRunner{}, func(*model.Task) (bool, string) {
		time.Sleep(60 * time.Millisecond)
		return true, ""
	})
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	archWorkerRole(t, d, "O", "a", model.StatusComplete)
	archWorkerRole(t, d, "O", "b", model.StatusComplete)
	archWorkerRole(t, d, "O", "c", model.StatusComplete)
	out, isErr := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"orchestrator":"O"}`, c.Worktree))
	testutil.Equal(t, isErr, false)
	testutil.Contains(t, out, "**not evaluated**: 2 role(s)")
	testutil.Equal(t, strings.Count(out, "**archived**: a"), 1)
}

func mustOrch(t *testing.T, d *db.DB, name string) int64 {
	t.Helper()
	o, err := d.HeraOrchestratorByName(name)
	testutil.NoError(t, err)
	return o.ID
}

func TestHeraArchive_SingleRoleByName(t *testing.T) {
	s, d := testHeraServer(t)
	s.SetHeraArchiver(&archRunner{}, func(*model.Task) (bool, string) { return true, "" })
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	task, role := archWorkerRole(t, d, "O", "done1", model.StatusComplete)
	out, isErr := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"role_name":"done1","orchestrator":"O"}`, c.Worktree))
	testutil.Equal(t, isErr, false)
	testutil.Contains(t, out, "**archived**: done1")
	g, _ := d.Get(task.ID)
	testutil.Equal(t, g.Archived, true)
	got, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.ArchivedAt != nil, true) // looked up by id: HeraRoleByName hides archived roles
}

// A sweep with two archivable roles archives BOTH (not just the first) and
// reports the refused one with its reason.
func TestHeraArchive_SweepArchivesAllArchivable(t *testing.T) {
	s, d := testHeraServer(t)
	s.SetHeraArchiver(&archRunner{}, func(task *model.Task) (bool, string) {
		if task.Worktree == "/wt/open-pr" {
			return false, "open PR"
		}
		return true, ""
	})
	c := seedCoordinator(t, s, d, "O", "/wt/coord")
	t1, _ := archWorkerRole(t, d, "O", "d1", model.StatusComplete)
	t2, _ := archWorkerRole(t, d, "O", "d2", model.StatusComplete)
	archWorkerRole(t, d, "O", "open-pr", model.StatusComplete)
	out, _ := archCall(t, s, fmt.Sprintf(`{"cwd":%q,"orchestrator":"O"}`, c.Worktree))
	testutil.Contains(t, out, "d1")
	testutil.Contains(t, out, "d2")
	testutil.Contains(t, out, "open-pr: git work not confirmed landed: open PR")
	for _, id := range []string{t1.ID, t2.ID} {
		g, _ := d.Get(id)
		testutil.Equal(t, g.Archived, true)
	}
}
