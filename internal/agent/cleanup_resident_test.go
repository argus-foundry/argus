package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// residentCleanupRepo builds a repo with a bare origin and a pushed worker
// branch checked out in an argus-managed worktree.
func residentCleanupRepo(t *testing.T) (root, origin, repo, wt, branch string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root = t.TempDir()
	origin = filepath.Join(root, "origin.git")
	residentCleanupGit(t, root, "init", "--bare", "-b", "master", origin)
	repo = filepath.Join(root, "repo")
	residentCleanupGit(t, root, "init", "-b", "master", repo)
	for _, kv := range [][]string{{"user.name", "Test"}, {"user.email", "t@example.com"}, {"commit.gpgsign", "false"}} {
		residentCleanupGit(t, repo, "config", kv[0], kv[1])
	}
	residentCleanupGit(t, repo, "commit", "--allow-empty", "-m", "init")
	residentCleanupGit(t, repo, "remote", "add", "origin", origin)
	branch = "argus/resident/w"
	wt = filepath.Join(root, ".argus", "worktrees", "p", "w")
	residentCleanupGit(t, repo, "worktree", "add", "-b", branch, wt)
	residentCleanupGit(t, wt, "commit", "--allow-empty", "-m", "w1")
	residentCleanupGit(t, repo, "push", "origin", branch)
	return root, origin, repo, wt, branch
}

// The resident variant removes the worktree and LOCAL branch only and hands
// back the origin target and its lease tip; the origin delete is a separate,
// bounded, leased call.
func TestRemoveWorktreeAndLocalBranch(t *testing.T) {
	root, origin, repo, wt, branch := residentCleanupRepo(t)
	pushed := strings.TrimSpace(residentCleanupGit(t, repo, "rev-parse", branch))
	// An unpushed local commit: the lease uses what origin was last seen at,
	// so the delete still goes through.
	residentCleanupGit(t, wt, "commit", "--allow-empty", "-m", "local only")

	dir, remote, tip := RemoveWorktreeAndLocalBranch(wt, branch, repo)
	testutil.Equal(t, dir, repo)
	testutil.Equal(t, remote, branch)
	testutil.Equal(t, tip, pushed)
	_, err := os.Stat(wt)
	testutil.True(t, os.IsNotExist(err))
	testutil.Equal(t, residentCleanupGit(t, repo, "branch", "--list", branch), "")
	// Still on origin until the separate delete runs.
	testutil.Contains(t, residentCleanupGit(t, origin, "branch", "--list", branch), branch)

	testutil.NoError(t, DeleteRemoteBranchContext(context.Background(), dir, remote, tip))
	testutil.Equal(t, residentCleanupGit(t, origin, "branch", "--list", branch), "")

	// The outcome is reported, not swallowed: a branch already gone from
	// origin is "gone", distinct from "changed; left in place".
	err = DeleteRemoteBranchContext(context.Background(), dir, remote, tip)
	testutil.ErrorIs(t, err, ErrRemoteBranchGone)
	testutil.False(t, errors.Is(err, ErrRemoteBranchChanged))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = DeleteRemoteBranchContext(ctx, dir, "argus/other", tip)
	testutil.ErrorIs(t, err, context.Canceled)
	testutil.NoError(t, DeleteRemoteBranchContext(context.Background(), dir, "", tip))
	// No lease, no delete.
	err = DeleteRemoteBranchContext(context.Background(), dir, remote, "")
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "left in place")

	// No branch: nothing to delete on origin.
	dir, remote, tip = RemoveWorktreeAndLocalBranch(filepath.Join(root, "missing"), "", repo)
	testutil.Equal(t, dir, "")
	testutil.Equal(t, remote, "")
	testutil.Equal(t, tip, "")
}

// A same-named branch re-created and pushed (a new resident worker with the
// same name) before the queued origin delete runs is left in place: the
// delete is leased on the tip captured at local removal.
func TestDeleteRemoteBranchContext_RecreatedBranchLeftInPlace(t *testing.T) {
	_, origin, repo, wt, branch := residentCleanupRepo(t)
	dir, remote, tip := RemoveWorktreeAndLocalBranch(wt, branch, repo)
	testutil.True(t, tip != "")

	wt2 := filepath.Join(filepath.Dir(wt), "w2")
	residentCleanupGit(t, repo, "worktree", "add", "-b", branch, wt2, "master")
	residentCleanupGit(t, wt2, "commit", "--allow-empty", "-m", "new worker")
	residentCleanupGit(t, repo, "push", "--force", "origin", branch)
	newTip := strings.TrimSpace(residentCleanupGit(t, repo, "rev-parse", branch))

	err := DeleteRemoteBranchContext(context.Background(), dir, remote, tip)
	testutil.ErrorIs(t, err, ErrRemoteBranchChanged)
	testutil.False(t, errors.Is(err, ErrRemoteBranchGone))
	testutil.Equal(t, strings.TrimSpace(residentCleanupGit(t, origin, "rev-parse", "refs/heads/"+branch)), newTip)
}

// TryLockRepo shares the per-repo cleanup lock with every other cleanup.
func TestTryLockRepo(t *testing.T) {
	repo := t.TempDir()
	unlock, ok := TryLockRepo(repo)
	testutil.True(t, ok)
	_, ok = TryLockRepo(repo)
	testutil.False(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LockRepoContext(ctx, repo)
	testutil.ErrorIs(t, err, context.Canceled)
	unlock()
	unlock2, err := LockRepoContext(context.Background(), repo)
	testutil.NoError(t, err)
	unlock2()
	unlock3, ok := TryLockRepo("")
	testutil.True(t, ok)
	unlock3()
}

// remoteBranchExists distinguishes "origin has no such branch" (false, nil)
// from "origin could not be asked" (false, err): an unreachable origin must
// never be logged as "already gone".
func TestRemoteBranchExists_UnreachableOrigin(t *testing.T) {
	repo := t.TempDir()
	residentCleanupGit(t, repo, "init", "-q")
	residentCleanupGit(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	present, err := remoteBranchExists(context.Background(), repo, "refs/heads/argus/x")
	testutil.False(t, present)
	testutil.Error(t, err)
}
