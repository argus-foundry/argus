package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// SamePath compares filesystem identities, including case and symlink aliases.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	if ai, err := os.Stat(a); err == nil {
		if bi, err := os.Stat(b); err == nil && os.SameFile(ai, bi) {
			return true
		}
	}
	return canonicalTaskPath(a) == canonicalTaskPath(b)
}
func canonicalTaskPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

// TaskGitRepositoryID identifies shared Git refs across linked checkouts.
func TaskGitRepositoryID(ctx context.Context, repo string) (string, error) {
	if repo == "" {
		return "", nil
	}
	common, _, err := RunTaskGit(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve Git common directory: %w", err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(repo, common)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return "", fmt.Errorf("resolve Git common directory: %w", err)
	}
	return filepath.Abs(common)
}

// LockTaskGitOwnership fences creation and cleanup across TUI and daemon
// processes. Take it before DB calls or the in-process cleanup mutex.
func LockTaskGitOwnership(ctx context.Context, repo string) (func(), error) {
	unlock, _, err := lockTaskGitOwnership(ctx, repo, false)
	return unlock, err
}

// TryLockTaskGitOwnership reports contention without waiting for a creator.
func TryLockTaskGitOwnership(ctx context.Context, repo string) (func(), bool, error) {
	return lockTaskGitOwnership(ctx, repo, true)
}

func lockTaskGitOwnership(ctx context.Context, repo string, try bool) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	identity, err := TaskGitRepositoryID(ctx, repo)
	if err != nil {
		uxlog.Log("[worktree] ownership identity failed repo=%q err=%v", repo, err)
		return nil, false, err
	}
	sum := sha256.Sum256([]byte(identity))
	dir := filepath.Join(db.DataDir(), "locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		uxlog.Log("[worktree] ownership lock directory failed: %v", err)
		return nil, false, err
	}
	file, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		uxlog.Log("[worktree] ownership lock open failed: %v", err)
		return nil, false, err
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, false, err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // G115: kernel file descriptors fit in int.
		if err == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, true, nil //nolint:gosec // G115: kernel file descriptors fit in int.
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			uxlog.Log("[worktree] ownership lock failed: %v", err)
			return nil, false, err
		}
		if try {
			file.Close()
			return nil, false, nil
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
}
