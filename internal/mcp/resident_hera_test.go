package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentDeleteGitWorktree(t *testing.T) {
	for _, tc := range []string{"merged", "empty", "unmerged", "force"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			repo := t.TempDir()
			run := func(dir string, args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			run(repo, "init", "-b", "master")
			run(repo, "config", "user.name", "Test")
			run(repo, "config", "user.email", "test@example.com")
			run(repo, "config", "commit.gpgsign", "false")
			run(repo, "commit", "--allow-empty", "-m", "initial")
			branch := "argus/resident/work"
			wt := filepath.Join(db.DataDir(), "worktrees", "p", "work")
			run(repo, "worktree", "add", "-b", branch, wt)
			testutil.NoError(t, d.SetProject("p", config.Project{Path: repo, Branch: "master"}))
			task := &model.Task{ID: "work", Name: "work", Project: "p", Branch: branch, BaseBranch: "master", Worktree: wt, Status: model.StatusInProgress}
			testutil.NoError(t, d.Add(task))
			_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker}, task.ID, wt)
			testutil.NoError(t, err)
			if tc != "empty" {
				for _, subject := range []string{"first change", "second change", "third change"} {
					run(wt, "commit", "--allow-empty", "-m", subject)
				}
			}
			if tc == "merged" {
				run(repo, "merge", "--ff-only", branch)
			}
			if tc == "force" {
				testutil.NoError(t, d.SetMeta(task.ID, "pr", "state", "awaiting-review"))
				for _, name := range []string{"dirty-one", "dirty-two"} {
					testutil.NoError(t, os.WriteFile(filepath.Join(wt, name), []byte("work"), 0600))
				}
			}
			// Empty work must succeed even with a not-safe verdict. The merged case
			// exercises the production local ancestor classifier.
			if tc == "empty" || tc == "unmerged" {
				s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
					return mergesafety.Verdict{Reason: "not merged"}, nil
				}
			}
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", fmt.Sprintf(`{"role":"work","force":%t}`, tc == "force"))
			testutil.Equal(t, isErr, tc == "unmerged")
			_, statErr := os.Stat(wt)
			if tc == "unmerged" {
				testutil.NoError(t, statErr)
				_, err = d.Get(task.ID)
				testutil.NoError(t, err)
			} else {
				testutil.True(t, os.IsNotExist(statErr))
				_, err = d.Get(task.ID)
				testutil.ErrorIs(t, err, db.ErrTaskNotFound)
				cmd := exec.Command("git", "show-ref", "--verify", "refs/heads/"+branch)
				cmd.Dir = repo
				testutil.True(t, cmd.Run() != nil)
			}
			if tc == "force" {
				for _, part := range []string{`"commits_ahead": "3"`, `"dirty_count": "2"`, "first change", "second change", "third change", branch, "awaiting-review"} {
					testutil.Contains(t, text, part)
				}
			}
		})
	}
}

func TestResidentHeraInventory(t *testing.T) {
	r := config.DefaultResidentConfig(100)
	defs := append(append([]Tool{}, heraToolDefs...), residentDeleteTool, residentGraduateTool)
	for _, tool := range defs {
		t.Run(tool.Name, func(t *testing.T) {
			name := "mcp__argus__" + tool.Name
			n := 0
			for _, tier := range [][]string{r.AllowTools, r.AskTools, r.DenyTools} {
				if slices.Contains(tier, name) {
					n++
				}
			}
			testutil.Equal(t, n, 1)
		})
	}
}

func TestResidentDeleteHoldsWorkerMutationLock(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	task, _, _ := residentTestWorker(t, d, o.ID, "worker")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan *Response, 1)
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		close(entered)
		<-release
		return mergesafety.Verdict{Reason: "work pending"}, nil
	}
	go func() { done <- s.toolHeraDeleteWorker(1, json.RawMessage(`{"role":"worker"}`), res.ID) }()
	<-entered // delete is inside classification, after validating live membership
	// Graduation must take this same lock before checking/ending membership.
	// TryLock is deterministic evidence, without a timing-based sleep/assertion.
	acquired := residentWorkerMutationMu.TryLock()
	if acquired {
		residentWorkerMutationMu.Unlock()
	}
	close(release)
	response := <-done
	testutil.False(t, acquired)
	testutil.True(t, callResult(t, response).IsError)
	residentWorkerMutationMu.Lock()
	// Simulate graduation's membership transaction while holding the same lock.
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(b.ID, "graduated"))
	newOrch, err := d.CreateHeraOrchestrator("graduate", "")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: newOrch.ID, Name: "coord", Kind: db.HeraKindCoordinator}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	residentWorkerMutationMu.Unlock()
	response = s.toolHeraDeleteWorker(1, json.RawMessage(`{"role":"worker","force":true}`), res.ID)
	testutil.True(t, callResult(t, response).IsError)
	still, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, still.Archived)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
}

func residentHeraFixture(t *testing.T) (*Server, *db.DB, *model.Task, *db.HeraOrchestrator) {
	t.Helper()
	s, d := testHeraServer(t)
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	s.taskStopper = agent.NewRunner(nil)
	return s, d, res, o
}

func residentTestWorker(t *testing.T, d *db.DB, oid int64, name string) (*model.Task, *db.HeraRole, *db.HeraBinding) {
	t.Helper()
	task := &model.Task{ID: name, Name: name, Project: "test-project", Worktree: t.TempDir(), Branch: "argus/resident/" + name, BaseBranch: "master", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	r, b, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: oid, Name: name, Kind: db.HeraKindWorker}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return task, r, b
}

func TestResidentDeleteIdentityAndScope(t *testing.T) {
	for _, tc := range []string{"unbound", "ordinary coordinator", "worker", "spoofed cwd", "other orchestrator", "self", "graduated"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			task, r, b := residentTestWorker(t, d, o.ID, "worker")
			caller := res.ID
			target := r.Name
			switch tc {
			case "unbound", "spoofed cwd":
				caller = ""
			case "ordinary coordinator":
				c := seedCoordinator(t, s, d, "other", "/wt/coord")
				caller = c.ID
			case "worker":
				caller = task.ID
			case "other orchestrator":
				other, err := d.CreateHeraOrchestrator("other", "")
				testutil.NoError(t, err)
				_, r, _ = residentTestWorker(t, d, other.ID, "outside")
				target = fmt.Sprint(r.ID)
			case "self":
				target = "resident"
			case "graduated":
				testutil.NoError(t, d.EndHeraBinding(b.ID, "graduated"))
			}
			args, _ := json.Marshal(map[string]interface{}{"role": target, "force": true, "cwd": res.Worktree, "id": res.ID})
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", string(args))
			testutil.True(t, isErr)
			testutil.True(t, len(text) > 0)
			still, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.False(t, still.Archived)
		})
	}
}

func TestResidentDeleteSafety(t *testing.T) {
	for _, tc := range []string{"merged", "unmerged", "classifier failure", "force unknown"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			task, r, _ := residentTestWorker(t, d, o.ID, "worker")
			calls := 0
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				calls++
				if tc == "classifier failure" {
					return mergesafety.Verdict{}, fmt.Errorf("network failed")
				}
				return mergesafety.Verdict{Safe: tc == "merged", Reason: "no merged PR"}, nil
			}
			force := tc == "force unknown"
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", fmt.Sprintf(`{"role":"worker","force":%t}`, force))
			testutil.Equal(t, isErr, tc == "unmerged" || tc == "classifier failure")
			got, err := d.HeraRole(r.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.NukedAt != nil, !isErr)
			if force {
				testutil.Equal(t, calls, 0)
				for _, part := range []string{task.Branch, "commits_ahead", "subjects", "dirty_count", "pr_state", "unknown (", "REPORT"} {
					testutil.Contains(t, text, part)
				}
			}
		})
	}
}

func TestResidentNewOrchestratorGuards(t *testing.T) {
	for _, tc := range []string{"resident", "live worker", "marker only", "reserved", "ordinary worker"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			task, _, b := residentTestWorker(t, d, o.ID, "worker")
			target := "new-team"
			cwd := task.Worktree
			switch tc {
			case "resident":
				cwd = res.Worktree
			case "marker only":
				testutil.NoError(t, d.EndHeraBinding(b.ID, "test"))
				testutil.NoError(t, d.SetMeta(task.ID, "hera", "resident_worker", "true"))
			case "reserved", "ordinary worker":
				testutil.NoError(t, d.EndHeraBinding(b.ID, "graduated"))
				if tc == "reserved" {
					target = "resident"
				} else {
					ordinary, err := d.CreateHeraOrchestrator("ordinary", "")
					testutil.NoError(t, err)
					_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: ordinary.ID, Name: "worker", Kind: db.HeraKindWorker}, task.ID, task.Worktree)
					testutil.NoError(t, err)
				}
			}
			args, _ := json.Marshal(map[string]string{"cwd": cwd, "name": target, "coordinator_role_name": "coord"})
			text, isErr := callTool(t, s, "hera_new_orchestrator", string(args))
			testutil.Equal(t, isErr, tc != "ordinary worker")
			if tc == "live worker" || tc == "marker only" {
				testutil.Contains(t, text, "graduate")
			}
		})
	}
}

func TestResidentDeleteListing(t *testing.T) {
	s, _, res, _ := residentHeraFixture(t)
	for _, caller := range []string{"", res.ID} {
		t.Run(caller, func(t *testing.T) {
			resp := s.handleToolsList(&Request{ID: 1, caller: caller})
			result := resp.Result.(ToolsListResult)
			found := false
			graduateFound := false
			for _, tool := range result.Tools {
				if tool.Name == "hera_delete_worker" {
					found = true
				}
				if tool.Name == "hera_graduate_worker" {
					graduateFound = true
				}
			}
			testutil.Equal(t, found, caller == res.ID)
			testutil.Equal(t, graduateFound, caller == res.ID)
		})
	}
}

func TestResidentBoundSpawnPinsStandingTeam(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	other := seedCoordinator(t, s, d, "other", "/wt/other")
	testutil.NoError(t, d.SetProject("test-project", config.Project{Path: t.TempDir()}))
	args := spawnArgs(other.Worktree, "do the errand", "errand", "test-project", "other")
	text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_spawn_worker", string(args))
	testutil.False(t, isErr)
	testutil.Contains(t, text, "**orchestrator**: resident")
	role, err := d.HeraRoleByName(o.ID, "errand")
	testutil.NoError(t, err)
	testutil.Equal(t, role.OrchestratorID, o.ID)
}

func TestBoundNewOrchestratorRejectsNullArguments(t *testing.T) {
	s, d := testHeraServer(t)
	task := seedCoordinator(t, s, d, "ordinary", "/wt/ordinary")
	response := s.toolHeraNewOrchestratorAs(1, json.RawMessage(`null`), task.ID)
	testutil.True(t, callResult(t, response).IsError)
}

func TestResidentSpawnProjectAndCap(t *testing.T) {
	for _, tc := range []string{"missing project", "unregistered project", "at cap", "freed slot", "configurable"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			// Register a repo path; the fake spawner avoids git and processes.
			testutil.NoError(t, d.SetProject("test-project", config.Project{Path: t.TempDir()}))
			_, _, b := residentTestWorker(t, d, o.ID, "first")
			testutil.NoError(t, d.SetConfigValue("resident.max_workers", "1"))
			project := "test-project"
			switch tc {
			case "missing project":
				project = ""
			case "unregistered project":
				project = "missing"
			case "freed slot":
				testutil.NoError(t, d.EndHeraBinding(b.ID, "graduated"))
			case "configurable":
				testutil.NoError(t, d.SetConfigValue("resident.max_workers", "2"))
			}
			args := spawnArgs(res.Worktree, "Do the errand", "next", project, "")
			resp := s.toolHeraSpawnWorker(1, args)
			cr := callResult(t, resp)
			testutil.Equal(t, cr.IsError, tc != "freed slot" && tc != "configurable")
		})
	}
}
