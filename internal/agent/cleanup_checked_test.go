package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G702: fixed Git binary with fixture arguments, no shell.
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func cleanupRepo(t *testing.T) (repo, origin string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "repo")
	testutil.NoError(t, os.MkdirAll(repo, 0o755))
	git(t, root, "init", "--bare", "-b", "master", origin)
	git(t, repo, "init", "-b", "master")
	git(t, repo, "commit", "--allow-empty", "-m", "init")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "origin", "master")
	return repo, origin
}

func addCleanupWorktree(t *testing.T, repo, name string, push bool) (wt, branch string) {
	t.Helper()
	wt = WorktreeDir("p", name)
	branch = "argus/" + name
	git(t, repo, "worktree", "add", "-b", branch, wt)
	if push {
		git(t, repo, "push", "origin", branch)
	}
	return wt, branch
}

func TestCleanupTaskGit(t *testing.T) {
	t.Run("removes worktree and both branches", func(t *testing.T) {
		repo, origin := cleanupRepo(t)
		wt, br := addCleanupWorktree(t, repo, "a", true)
		res := CleanupTaskGit(wt, br, repo)
		testutil.Equal(t, res.Worktree, CleanupRemoved)
		testutil.Equal(t, res.LocalBranch, CleanupRemoved)
		testutil.Equal(t, res.RemoteBranch, CleanupRemoved)
		testutil.Equal(t, res.Failed(), false)
		_, err := os.Stat(wt)
		testutil.True(t, os.IsNotExist(err))
		cmd := exec.Command("git", "--git-dir", origin, "rev-parse", "--verify", "--quiet", "refs/heads/"+br)
		testutil.True(t, cmd.Run() != nil)
	})
	t.Run("already gone reports absent and is repeatable", func(t *testing.T) {
		repo, _ := cleanupRepo(t)
		wt, br := addCleanupWorktree(t, repo, "b", false)
		CleanupTaskGit(wt, br, repo)
		res := CleanupTaskGit(wt, br, repo)
		testutil.Equal(t, res.Worktree, CleanupAbsent)
		testutil.Equal(t, res.LocalBranch, CleanupAbsent)
		testutil.Equal(t, res.RemoteBranch, CleanupAbsent)
	})
	t.Run("no origin means remote not applicable", func(t *testing.T) {
		repo, _ := cleanupRepo(t)
		git(t, repo, "remote", "remove", "origin")
		wt, br := addCleanupWorktree(t, repo, "c", false)
		res := CleanupTaskGit(wt, br, repo)
		testutil.Equal(t, res.RemoteBranch, CleanupNotApplicable)
		testutil.Equal(t, res.LocalBranch, CleanupRemoved)
	})
	t.Run("unmanaged path is never removed", func(t *testing.T) {
		repo, _ := cleanupRepo(t)
		foreign := filepath.Join(t.TempDir(), "keepme")
		testutil.NoError(t, os.MkdirAll(foreign, 0o755))
		res := CleanupTaskGit(foreign, "argus/keep", repo)
		testutil.Equal(t, res.Worktree, CleanupFailed)
		testutil.True(t, res.Failed())
		testutil.Equal(t, res.LocalBranch, CleanupNotApplicable)
		_, err := os.Stat(foreign)
		testutil.NoError(t, err)
	})
	t.Run("non-argus branch without worktree is skipped", func(t *testing.T) {
		repo, _ := cleanupRepo(t)
		res := CleanupTaskGit("", "master", repo)
		testutil.Equal(t, res.LocalBranch, CleanupNotApplicable)
		testutil.Equal(t, res.Worktree, CleanupNotApplicable)
		git(t, repo, "rev-parse", "--verify", "refs/heads/master")
	})
	t.Run("unreachable remote fails remote check", func(t *testing.T) {
		repo, origin := cleanupRepo(t)
		wt, br := addCleanupWorktree(t, repo, "d", false)
		testutil.NoError(t, os.RemoveAll(origin))
		res := CleanupTaskGit(wt, br, repo)
		testutil.Equal(t, res.RemoteBranch, CleanupFailed)
		testutil.Equal(t, res.LocalBranch, CleanupRemoved)
		testutil.True(t, res.Failed())
		testutil.Contains(t, res.Err, "remote branch check failed")
	})
}

func TestCleanupTaskGit_SerializesPerRepo(t *testing.T) {
	repo, _ := cleanupRepo(t)
	wt, br := addCleanupWorktree(t, repo, "e", false)
	unlock := lockRepo(repo)
	done := make(chan struct{})
	go func() {
		CleanupTaskGit(wt, br, repo)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("cleanup ran while the repo lock was held")
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cleanup did not run after unlock")
	}
}
