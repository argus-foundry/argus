package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskGitOwnership_RepositoryAliasesAndCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	git(t, repo, "init")
	alias := filepath.Join(t.TempDir(), "alias")
	testutil.NoError(t, os.Symlink(repo, alias))
	synctest.Test(t, func(t *testing.T) {
		unlock, err := LockTaskGitOwnership(context.Background(), repo)
		testutil.NoError(t, err)
		defer unlock()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			release, err := LockTaskGitOwnership(ctx, alias)
			if release != nil {
				release()
			}
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("alias escaped fence: %v", err)
		default:
		}
		cancel()
		testutil.ErrorIs(t, <-done, context.Canceled)
	})
	testutil.True(t, SamePath(repo, alias))
	testutil.True(t, SamePath(repo, repo+"/."))
	testutil.Equal(t, SamePath(repo, ""), false)
}

func TestTaskGitOwnership_CaseAliases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := t.TempDir()
	repo, alias := filepath.Join(parent, "CaseRepo"), filepath.Join(parent, "caserepo")
	testutil.NoError(t, os.Mkdir(repo, 0700))
	git(t, repo, "init")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("filesystem is case sensitive")
	}
	testutil.True(t, SamePath(repo, alias))
	unlock, err := LockTaskGitOwnership(context.Background(), repo)
	testutil.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	second, err := LockTaskGitOwnership(ctx, alias)
	if second != nil {
		second()
	}
	testutil.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTaskGitOwnership_LinkedCheckouts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, _ := cleanupRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "linked", linked)
	testutil.Equal(t, SamePath(repo, linked), false)
	unlock, err := LockTaskGitOwnership(context.Background(), repo)
	testutil.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	second, err := LockTaskGitOwnership(ctx, linked)
	if second != nil {
		second()
	}
	testutil.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTaskGitOwnership_LockDirectoryFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, _ := cleanupRepo(t)
	data := filepath.Join(os.Getenv("HOME"), ".argus")
	testutil.NoError(t, os.MkdirAll(data, 0700))
	testutil.NoError(t, os.WriteFile(filepath.Join(data, "locks"), []byte("obstruct"), 0600))
	unlock, err := LockTaskGitOwnership(context.Background(), repo)
	if unlock != nil {
		unlock()
	}
	testutil.True(t, err != nil)
	testutil.True(t, unlock == nil)
}
