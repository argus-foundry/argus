package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func residentCleanupGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// The resident variant removes the worktree and LOCAL branch only and hands
// back the origin target; the origin delete is a separate, bounded call.
func TestRemoveWorktreeAndLocalBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	residentCleanupGit(t, root, "init", "--bare", "-b", "master", origin)
	repo := filepath.Join(root, "repo")
	residentCleanupGit(t, root, "init", "-b", "master", repo)
	for _, kv := range [][]string{{"user.name", "Test"}, {"user.email", "t@example.com"}, {"commit.gpgsign", "false"}} {
		residentCleanupGit(t, repo, "config", kv[0], kv[1])
	}
	residentCleanupGit(t, repo, "commit", "--allow-empty", "-m", "init")
	residentCleanupGit(t, repo, "remote", "add", "origin", origin)
	branch := "argus/resident/w"
	wt := filepath.Join(root, ".argus", "worktrees", "p", "w")
	residentCleanupGit(t, repo, "worktree", "add", "-b", branch, wt)
	residentCleanupGit(t, repo, "push", "origin", branch)

	dir, remote := RemoveWorktreeAndLocalBranch(wt, branch, repo)
	testutil.Equal(t, dir, repo)
	testutil.Equal(t, remote, branch)
	_, err := os.Stat(wt)
	testutil.True(t, os.IsNotExist(err))
	testutil.Equal(t, residentCleanupGit(t, repo, "branch", "--list", branch), "")
	// Still on origin until the separate delete runs.
	testutil.Contains(t, residentCleanupGit(t, origin, "branch", "--list", branch), branch)

	testutil.NoError(t, DeleteRemoteBranchContext(context.Background(), dir, remote))
	testutil.Equal(t, residentCleanupGit(t, origin, "branch", "--list", branch), "")

	// The outcome is reported, not swallowed.
	err = DeleteRemoteBranchContext(context.Background(), dir, remote)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "git push origin --delete")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = DeleteRemoteBranchContext(ctx, dir, "argus/other")
	testutil.ErrorIs(t, err, context.Canceled)
	testutil.NoError(t, DeleteRemoteBranchContext(context.Background(), dir, ""))

	// No branch: nothing to delete on origin.
	dir, remote = RemoveWorktreeAndLocalBranch(filepath.Join(root, "missing"), "", repo)
	testutil.Equal(t, dir, "")
	testutil.Equal(t, remote, "")
}
