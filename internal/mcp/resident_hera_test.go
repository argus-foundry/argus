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
	for _, tc := range []string{"merged", "empty", "unmerged", "dirty only", "force"} {
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
			_, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "work", Kind: db.HeraKindWorker}, task.ID, wt)
			testutil.NoError(t, err)
			if tc != "empty" && tc != "dirty only" {
				for _, subject := range []string{"first change", "second change", "third change"} {
					run(wt, "commit", "--allow-empty", "-m", subject)
				}
			}
			if tc == "merged" {
				run(repo, "merge", "--ff-only", branch)
			}
			if tc == "dirty only" {
				// Zero commits ahead but an uncommitted file is not empty work.
				testutil.NoError(t, os.WriteFile(filepath.Join(wt, "scratch"), []byte("work"), 0600))
			}
			if tc == "force" {
				testutil.NoError(t, d.SetMeta(task.ID, "pr", "state", "awaiting-review"))
				for _, name := range []string{"dirty-one", "dirty-two"} {
					testutil.NoError(t, os.WriteFile(filepath.Join(wt, name), []byte("work"), 0600))
				}
			}
			// Empty work must succeed even with a not-safe verdict. The merged case
			// exercises the production local ancestor classifier.
			if tc == "empty" || tc == "unmerged" || tc == "dirty only" {
				s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
					return mergesafety.Verdict{Reason: "not merged"}, nil
				}
			}
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", fmt.Sprintf(`{"role":"work","force":%t}`, tc == "force"))
			refused := tc == "unmerged" || tc == "dirty only"
			testutil.Equal(t, isErr, refused)
			_, statErr := os.Stat(wt)
			if refused {
				testutil.Contains(t, text, "not safe")
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
				assertNoResidentNotice(t, d)
			}
			if tc == "dirty only" {
				testutil.Contains(t, text, "dirty files: 1")
				_, statErr = os.Stat(filepath.Join(wt, "scratch"))
				testutil.NoError(t, statErr)
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
	s.heraSpawn = residentAwareFakeSpawn(d)
	armResidentToken(t, s)
	// Registered after the DB's own cleanup, so it runs first: async reclaim
	// sweeps finish before the DB closes.
	t.Cleanup(s.residentBackground.Wait)
	return s, d, res, o
}

// residentAwareFakeSpawn mirrors agent.SpawnHeraWorker's split: the standing
// team gets the privileged resident-worker insert, every other orchestrator
// the ordinary (guarded) one.
func residentAwareFakeSpawn(d *db.DB) HeraSpawner {
	ordinary := fakeHeraSpawn(d)
	return func(in HeraSpawnInput) (*HeraSpawnResult, error) {
		yes, err := d.IsResidentOrchestrator(in.OrchestratorID)
		if err != nil || !yes {
			return ordinary(in)
		}
		name, err := d.UniqueHeraRoleName(in.OrchestratorID, in.BaseName)
		if err != nil {
			return nil, err
		}
		task := &model.Task{Name: name, Status: model.StatusInProgress, Project: in.Project, Worktree: "/wt/spawn-" + name, Prompt: in.TaskPrompt}
		if err := d.Add(task); err != nil {
			return nil, err
		}
		role, binding, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: in.OrchestratorID, Name: name, Kind: db.HeraKindWorker, ArgusProject: in.Project}, task.ID, task.Worktree)
		if err != nil {
			return nil, err
		}
		return &HeraSpawnResult{Task: task, Role: role, Binding: binding}, nil
	}
}

// residentTestWorker seeds a live worker under oid: the privileged insert for
// the standing team, the ordinary one for any other orchestrator.
func residentTestWorker(t *testing.T, d *db.DB, oid int64, name string) (*model.Task, *db.HeraRole, *db.HeraBinding) {
	t.Helper()
	// A clean (empty) git repo: hera_delete_worker refuses an unknown dirty count.
	wt := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", wt).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	task := &model.Task{ID: name, Name: name, Project: "test-project", Worktree: wt, Branch: "argus/resident/" + name, BaseBranch: "master", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	create := d.CreateHeraRoleWithBinding
	if yes, err := d.IsResidentOrchestrator(oid); err == nil && yes {
		create = d.CreateResidentWorkerRoleWithBinding
	}
	r, b, err := create(db.CreateHeraRoleInput{OrchestratorID: oid, Name: name, Kind: db.HeraKindWorker}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return task, r, b
}

// assertNoResidentNotice proves the resident's own delete is not echoed back
// to it through the human-mutation outbox.
func assertNoResidentNotice(t *testing.T, d *db.DB) {
	t.Helper()
	notices, err := d.PendingResidentNotices(50)
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 0)
}

func TestResidentDeleteIdentityAndScope(t *testing.T) {
	const residentOnly = "is resident only"
	for _, tc := range []struct{ name, reason string }{
		{"unbound", residentOnly},
		// Naming the resident's task id in any task-id argument is refused centrally.
		{"spoofed id", "resident identity requires the resident's own bound connection"},
		// cwd naming the resident is refused centrally before dispatch.
		{"spoofed cwd", "resident identity requires the resident's own bound connection"},
		// Each non-resident bound caller targets a SIBLING, so only
		// boundResident's IsResident check can refuse it (self-target checks and
		// the standing-binding lookup would let a worker through).
		{"worker targets sibling", residentOnly},
		{"worker targets itself", residentOnly},
		{"ordinary coordinator", residentOnly},
		{"other orchestrator", "target is not a live worker of the resident orchestrator"},
		{"self", "resident delete must never target itself"},
		{"graduated", "possibly graduated"},
		{"success", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			task, r, b := residentTestWorker(t, d, o.ID, "worker")
			sibling, _, _ := residentTestWorker(t, d, o.ID, "sibling")
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true}, nil
			}
			caller := res.ID
			target := r.Name
			args := map[string]interface{}{"force": true}
			switch tc.name {
			case "unbound":
				caller = ""
			case "spoofed id":
				caller = ""
				args["id"] = res.ID
			case "spoofed cwd":
				caller = ""
				args["cwd"] = res.Worktree
			case "ordinary coordinator":
				c := seedCoordinator(t, s, d, "other", "/wt/coord")
				caller = c.ID
			case "worker targets sibling":
				caller, target = task.ID, sibling.Name
			case "worker targets itself":
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
			args["role"] = target
			raw, _ := json.Marshal(args)
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", string(raw))
			if tc.reason == "" {
				testutil.False(t, isErr)
				testutil.Contains(t, text, "Deleted worker worker")
				got, err := d.HeraRole(r.ID)
				testutil.NoError(t, err)
				testutil.True(t, got.NukedAt != nil)
				assertNoResidentNotice(t, d)
			} else {
				testutil.True(t, isErr)
				testutil.Contains(t, text, tc.reason)
				for _, id := range []string{task.ID, sibling.ID} {
					still, err := d.Get(id)
					testutil.NoError(t, err)
					testutil.True(t, still != nil && !still.Archived)
				}
			}
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
			if !isErr {
				assertNoResidentNotice(t, d)
			}
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
	s, d, res, o := residentHeraFixture(t)
	worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
	coord := seedCoordinator(t, s, d, "other", "/wt/coord")
	for _, caller := range []string{"", res.ID, worker.ID, coord.ID} {
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
	for _, tc := range []string{"missing project", "unregistered project", "at cap", "freed slot", "configurable", "zero cap", "negative cap"} {
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
			case "zero cap":
				// An invalid cap falls back to the default of 8.
				testutil.NoError(t, d.SetConfigValue("resident.max_workers", "0"))
			case "negative cap":
				// Settings accepts any integer; below 1 still means 8.
				testutil.NoError(t, d.SetConfigValue("resident.max_workers", "-1"))
			}
			// Through the JSON-RPC dispatch as the bound resident: an unbound
			// connection naming the resident's cwd is refused (see
			// TestResidentImpersonationRefused).
			args := spawnArgs(res.Worktree, "Do the errand", "next", project, "")
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_spawn_worker", string(args))
			ok := tc == "freed slot" || tc == "configurable" || tc == "zero cap" || tc == "negative cap"
			testutil.Equal(t, isErr, !ok)
			_, err := d.HeraRoleByName(o.ID, "next")
			testutil.Equal(t, err == nil, ok)
			if !ok {
				testutil.Contains(t, text, map[string]string{"missing project": "project", "unregistered project": "project", "at cap": "max_workers"}[tc])
			}
		})
	}
}
