package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCleanupTaskGit_ManagedContainment(t *testing.T) {
	for _, kind := range []string{"parent symlink", "foreign marker", "unregistered", "wrong repository"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, _ := cleanupRepo(t)
			root := filepath.Join(os.Getenv("HOME"), ".argus", "worktrees")
			testutil.NoError(t, os.MkdirAll(root, 0700))
			victim := filepath.Join(t.TempDir(), "victim")
			testutil.NoError(t, os.MkdirAll(victim, 0700))
			candidate := victim
			switch kind {
			case "parent symlink":
				link := filepath.Join(root, "escape")
				testutil.NoError(t, os.Symlink(filepath.Dir(victim), link))
				candidate = filepath.Join(link, "victim")
			case "foreign marker":
				candidate = filepath.Join(t.TempDir(), ".argus", "worktrees", "p", "victim")
				testutil.NoError(t, os.MkdirAll(candidate, 0700))
				victim = candidate
			case "unregistered":
				candidate = filepath.Join(root, "p", "victim")
				testutil.NoError(t, os.MkdirAll(candidate, 0700))
				victim = candidate
			case "wrong repository":
				home := os.Getenv("HOME")
				other, _ := cleanupRepo(t)
				t.Setenv("HOME", home)
				candidate = filepath.Join(root, "p", "victim")
				git(t, other, "worktree", "add", "-b", "argus/victim", candidate)
				victim = candidate
			}
			sentinel := filepath.Join(victim, "keep")
			testutil.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
			res := CleanupTaskGit(candidate, "argus/victim", repo)
			testutil.Equal(t, res.Worktree, CleanupFailed)
			_, err := os.Stat(sentinel)
			testutil.NoError(t, err)
		})
	}
}

func TestCleanupTaskGit_RemoteDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real Git subprocess deadline")
	}
	t.Setenv("HOME", t.TempDir())
	repo, _ := cleanupRepo(t)
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	bin := t.TempDir()
	release := filepath.Join(bin, "release")
	entered := filepath.Join(bin, "entered")
	script := "#!/bin/sh\nif [ \"$1\" = ls-remote ]; then\n touch '" + entered + "'\n while [ ! -f '" + release + "' ]; do sleep 0.1; done\nfi\nexec '" + realGit + "' \"$@\"\n"
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	done := make(chan GitCleanupResult, 1)
	go func() { done <- CleanupTaskGit("", "argus/stall", repo) }()
	select {
	case res := <-done:
		testutil.Equal(t, res.RemoteBranch, CleanupFailed)
	case <-time.After(18 * time.Second):
		testutil.NoError(t, os.WriteFile(release, nil, 0600))
		<-done
		t.Fatal("remote check exceeded deadline")
	}
	_, err = os.Stat(entered)
	testutil.NoError(t, err)
	// A subsequent cleanup can take the same repo lock after timeout.
	testutil.NoError(t, os.WriteFile(release, nil, 0600))
	res := CleanupTaskGit("", "argus/stall", repo)
	testutil.Equal(t, res.RemoteBranch, CleanupAbsent)
}

func TestCleanupTaskGit_CanceledRemoteDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises real Git subprocess cancellation")
	}
	t.Setenv("HOME", t.TempDir())
	repo, origin := cleanupRepo(t)
	_, branch := addCleanupWorktree(t, repo, "cancel-push", true)
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = push ]; then sleep 30; exit 0; fi\nexec '" + realGit + "' \"$@\"\n"
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	res := CleanupTaskGitContext(ctx, "", branch, repo)
	testutil.Equal(t, res.RemoteBranch, CleanupFailed)
	testutil.Contains(t, res.Err, "remote branch deletion interrupted")
	testutil.True(t, time.Since(started) < 5*time.Second)
	git(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
}

func TestCleanupTaskGit_CancelsLegacyLockWait(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, _ := cleanupRepo(t)
	legacyUnlock := lockRepo(repo)
	defer legacyUnlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		unlock, err := LockTaskGitOwnership(ctx, repo)
		if err != nil {
			return
		}
		defer unlock()
		close(entered)
		CleanupTaskGitContext(ctx, "", "argus/unused", repo)
	}()
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled cleanup retained ownership while waiting on legacy mutex")
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	unlock, err := LockTaskGitOwnership(lockCtx, repo)
	testutil.NoError(t, err)
	if unlock != nil {
		unlock()
	}
}

func TestCleanupTaskGit_RegisteredOutsideRoot(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo, origin := cleanupRepo(t)
			victim := filepath.Join(t.TempDir(), "outside")
			branch := "argus/outside"
			git(t, repo, "worktree", "add", "-b", branch, victim)
			git(t, repo, "push", "origin", branch)
			sentinel := filepath.Join(victim, "keep")
			testutil.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
			candidate := victim
			if symlink {
				root := filepath.Dir(WorktreeDir("escape", "outside"))
				testutil.NoError(t, os.MkdirAll(filepath.Dir(root), 0700))
				testutil.NoError(t, os.Symlink(filepath.Dir(victim), root))
				candidate = filepath.Join(root, "outside")
			}
			res := CleanupTaskGit(candidate, branch, repo)
			testutil.Equal(t, res.Worktree, CleanupFailed)
			_, err := os.Stat(sentinel)
			testutil.NoError(t, err)
			git(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
			git(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
		})
	}
}
