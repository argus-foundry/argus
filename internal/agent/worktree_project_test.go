package agent

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBranchExistsInProject(t *testing.T) {
	repo := initGitRepo(t)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		testutil.NoError(t, cmd.Run())
	}
	run("branch", "local")
	run("update-ref", "refs/remotes/upstream/tracked", "HEAD")
	run("update-ref", "refs/remotes/origin/origin-only", "HEAD")
	for _, tc := range []struct {
		branch string
		want   bool
	}{
		{"HEAD", true}, {"local", true}, {"tracked", true}, {"origin-only", true}, {"missing", false}, {"", false},
	} {
		t.Run(tc.branch, func(t *testing.T) {
			testutil.Equal(t, BranchExistsInProject(repo, tc.branch), tc.want)
		})
	}
	testutil.Equal(t, BranchExistsInProject("", "local"), false)
	// A failed fetch must not discard a valid local branch.
	run("remote", "add", "origin", t.TempDir()+"/absent")
	testutil.Equal(t, BranchExistsInProject(repo, "local"), true)
}

func TestBranchExistsInProject_FetchesRemote(t *testing.T) {
	remote := initGitRepo(t)
	repo := initGitRepo(t)
	addRemote := exec.Command("git", "remote", "add", "origin", remote)
	addRemote.Dir = repo
	testutil.NoError(t, addRemote.Run())
	cmd := exec.Command("git", "branch", "fresh")
	cmd.Dir = remote
	testutil.NoError(t, cmd.Run())
	testutil.Equal(t, isValidRef(repo, "origin/fresh"), false)
	testutil.Equal(t, BranchExistsInProject(repo, "fresh"), true)
}

func TestSpawnHeraWorker_ProjectDefaultAndExplicitMissing(t *testing.T) {
	for _, branch := range []string{"", "missing"} {
		t.Run("branch="+branch, func(t *testing.T) {
			repo := initGitRepo(t)
			d := createTestDB(t, repo)
			cmd := exec.Command("git", "branch", "target-default")
			cmd.Dir = repo
			testutil.NoError(t, cmd.Run())
			// Move HEAD beyond the configured default to prove ancestry selection.
			cmd = exec.Command("git", "commit", "--allow-empty", "-qm", "coordinator work")
			cmd.Dir = repo
			testutil.NoError(t, cmd.Run())
			testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "target-default"}))
			orch, err := d.CreateHeraOrchestrator("orch", "missing-coordinator-base")
			testutil.NoError(t, err)
			runner := &fakeRunner{}
			res, err := SpawnHeraWorker(d, runner, HeraWorkerSpawnInput{
				OrchestratorID: orch.ID, BaseName: "worker", Project: "proj", Branch: branch, TaskPrompt: "work",
			})
			if branch != "" {
				testutil.Equal(t, err != nil, true)
				testutil.Contains(t, err.Error(), "invalid reference")
				testutil.Equal(t, runner.startCalls, 0)
				return
			}
			testutil.NoError(t, err)
			t.Cleanup(func() { RemoveWorktreeAndBranch(res.Task.Worktree, res.Task.Branch, repo) })
			testutil.Equal(t, res.Task.BaseBranch, "target-default")
			cmd = exec.Command("git", "rev-parse", "target-default", res.Task.Branch)
			cmd.Dir = repo
			out, err := cmd.Output()
			testutil.NoError(t, err)
			lines := strings.Fields(string(out))
			testutil.Equal(t, len(lines), 2)
			testutil.Equal(t, lines[0], lines[1])
		})
	}
}
