package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// TaskReferencesGitBranch matches the task's own branch and supported forms of
// its base ref. Callers check repository identity separately.
func TaskReferencesGitBranch(task *model.Task, branch string) bool {
	if branch == "" {
		return false
	}
	return task.Branch == branch || task.BaseBranch == branch || task.BaseBranch == "origin/"+branch ||
		task.BaseBranch == "refs/heads/"+branch || task.BaseBranch == "refs/remotes/origin/"+branch
}

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

// TaskGitWorkingDirectory resolves a repository alias once so the caller can
// use the same directory for Git ownership, validation, and commands.
func TaskGitWorkingDirectory(repo string) string {
	if repo == "" {
		return ""
	}
	return canonicalTaskPath(repo)
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

// TaskGitRepositoryIDForTask checks the live checkout before project
// configuration, which can change without moving the checkout.
func TaskGitRepositoryIDForTask(ctx context.Context, task *model.Task, cfg config.Config) (string, error) {
	if task.Worktree != "" {
		if _, err := os.Lstat(task.Worktree); err == nil {
			if _, err := os.Lstat(filepath.Join(task.Worktree, ".git")); err != nil {
				return "", err
			}
			return TaskGitRepositoryID(ctx, task.Worktree)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return TaskGitRepositoryID(ctx, ResolveDir(task, cfg))
}

// TaskGitRepositoryStamp distinguishes replacement repositories at an existing
// common-directory pathname. The resolved path itself is checked separately.
func TaskGitRepositoryStamp(commonDir string) (string, error) {
	if commonDir == "" {
		return "", nil
	}
	info, err := os.Stat(commonDir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("unable to identify the Git common directory")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

// TaskGitRepository identifies the repository a cleanup decision was made against.
type TaskGitRepository struct {
	Directory string
	CommonDir string
	Stamp     string
	Origin    string
}

// CaptureTaskGitRepository captures the execution directory and common Git identity.
func CaptureTaskGitRepository(ctx context.Context, directory string) (TaskGitRepository, error) {
	directory = TaskGitWorkingDirectory(directory)
	if directory == "" {
		return TaskGitRepository{}, errors.New("missing Git repository directory")
	}
	common, err := TaskGitRepositoryID(ctx, directory)
	if err != nil {
		return TaskGitRepository{}, err
	}
	stamp, err := TaskGitRepositoryStamp(common)
	if err != nil {
		return TaskGitRepository{}, err
	}
	return TaskGitRepository{Directory: directory, CommonDir: common, Stamp: stamp, Origin: TaskGitOriginStamp(ctx, directory)}, nil
}

// TaskGitOriginStamp fingerprints the fetch and push destinations without
// persisting or logging URLs that may carry credentials.
func TaskGitOriginStamp(ctx context.Context, repoDir string) string {
	stamp, _ := taskGitOrigin(ctx, repoDir)
	return stamp
}

func taskGitOrigin(ctx context.Context, repoDir string) (string, []string) {
	if repoDir == "" {
		return "", nil
	}
	fetch, _, fetchErr := RunTaskGit(ctx, repoDir, "remote", "get-url", "--all", "origin")
	push, _, pushErr := RunTaskGit(ctx, repoDir, "remote", "get-url", "--push", "--all", "origin")
	if fetchErr != nil || pushErr != nil || fetch == "" || push == "" {
		return "", nil
	}
	hash := sha256.Sum256([]byte(fetch + "\x00" + push))
	return hex.EncodeToString(hash[:]), strings.Split(push, "\n")
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
