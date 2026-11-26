package db

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestLockSessionMutationWaitsForReleaseOrCancellation(t *testing.T) {
	d := testDB(t)
	synctest.Test(t, func(t *testing.T) {
		unlock, acquired, err := d.TryLockSessionMutation("task")
		testutil.NoError(t, err)
		testutil.True(t, acquired)
		defer unlock()

		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()
		release, err := d.LockSessionMutation(ctx, "task")
		testutil.Nil(t, release)
		testutil.ErrorIs(t, err, context.DeadlineExceeded)

		results := make(chan error, 1)
		go func() {
			release, err := d.LockSessionMutation(t.Context(), "task")
			if release != nil {
				release()
			}
			results <- err
		}()
		synctest.Wait()
		testutil.Equal(t, len(results), 0)
		unlock()
		testutil.NoError(t, <-results)
	})
}

func TestTryLockSessionMutationDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sql")
	first := openSessionLockTestDB(t, path)
	alias := filepath.Join(t.TempDir(), "alias.sql")
	testutil.NoError(t, os.Symlink(path, alias))
	second := openSessionLockTestDB(t, alias)
	unrelated := openSessionLockTestDB(t, filepath.Join(filepath.Dir(path), "other.sql"))

	taskID := "../../task"
	unlock, acquired, err := first.TryLockSessionMutation(taskID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(unlock)

	for name, handle := range map[string]*DB{"same handle": first, "symlink alias": second} {
		t.Run(name, func(t *testing.T) {
			blockedUnlock, acquired, err := handle.TryLockSessionMutation(taskID)
			testutil.NoError(t, err)
			testutil.False(t, acquired)
			testutil.Nil(t, blockedUnlock)
		})
	}

	otherTaskUnlock, acquired, err := second.TryLockSessionMutation("other task")
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(otherTaskUnlock)

	unrelatedUnlock, acquired, err := unrelated.TryLockSessionMutation(taskID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(unrelatedUnlock)

	unlock()
	nextUnlock, acquired, err := second.TryLockSessionMutation(taskID)
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(nextUnlock)
	unlock()
	_, acquired, err = first.TryLockSessionMutation(taskID)
	testutil.NoError(t, err)
	testutil.False(t, acquired)

	files, err := os.ReadDir(first.sessionLockDir)
	testutil.NoError(t, err)
	testutil.Equal(t, len(files), 2)
	dir, err := os.Stat(first.sessionLockDir)
	testutil.NoError(t, err)
	testutil.Equal(t, dir.Mode().Perm()&0077, 0)
	for _, file := range files {
		testutil.Equal(t, len(file.Name()), 64+len(".lock"))
		info, err := file.Info()
		testutil.NoError(t, err)
		testutil.Equal(t, info.Mode().Perm()&0077, 0)
	}
}

func TestTryLockSessionMutationMemory(t *testing.T) {
	first, second := testDB(t), testDB(t)
	_, acquired, err := first.TryLockSessionMutation("")
	testutil.Error(t, err)
	testutil.False(t, acquired)

	unlock, acquired, err := first.TryLockSessionMutation("task")
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(unlock)

	_, acquired, err = first.TryLockSessionMutation("task")
	testutil.NoError(t, err)
	testutil.False(t, acquired)
	otherUnlock, acquired, err := second.TryLockSessionMutation("task")
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(otherUnlock)

	unlock()
	nextUnlock, acquired, err := first.TryLockSessionMutation("task")
	testutil.NoError(t, err)
	testutil.True(t, acquired)
	t.Cleanup(nextUnlock)
	unlock()
	_, acquired, err = first.TryLockSessionMutation("task")
	testutil.NoError(t, err)
	testutil.False(t, acquired)
}

func TestTryLockSessionMutationInvalidPaths(t *testing.T) {
	t.Run("directory occupied by a file", func(t *testing.T) {
		d := openSessionLockTestDB(t, filepath.Join(t.TempDir(), "data.sql"))
		testutil.NoError(t, os.WriteFile(d.sessionLockDir, nil, 0600))
		unlock, acquired, err := d.TryLockSessionMutation("task")
		testutil.ErrorIs(t, err, syscall.ENOTDIR)
		testutil.False(t, acquired)
		testutil.Nil(t, unlock)
	})

	t.Run("lock file replaced by a symlink", func(t *testing.T) {
		d := openSessionLockTestDB(t, filepath.Join(t.TempDir(), "data.sql"))
		unlock, acquired, err := d.TryLockSessionMutation("task")
		testutil.NoError(t, err)
		testutil.True(t, acquired)
		unlock()

		files, err := os.ReadDir(d.sessionLockDir)
		testutil.NoError(t, err)
		testutil.Equal(t, len(files), 1)
		path := filepath.Join(d.sessionLockDir, files[0].Name())
		testutil.NoError(t, os.Remove(path))
		testutil.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "other.lock"), path))

		unlock, acquired, err = d.TryLockSessionMutation("task")
		testutil.ErrorIs(t, err, syscall.ELOOP)
		testutil.False(t, acquired)
		testutil.Nil(t, unlock)
	})
}

func openSessionLockTestDB(t *testing.T, path string) *DB {
	t.Helper()
	d, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}
