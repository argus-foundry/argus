package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// Cleanup field outcomes reported by CleanupTaskGit.
const (
	CleanupRemoved       = "removed"
	CleanupAbsent        = "absent"
	CleanupFailed        = "failed"
	CleanupNotApplicable = "not_applicable"
)

// GitCleanupResult is the independently verified outcome of removing a task's
// worktree and branches. Err is empty unless a field is CleanupFailed.
type GitCleanupResult struct {
	Worktree     string
	LocalBranch  string
	RemoteBranch string
	Err          string
}

// Failed reports whether any field failed verification.
func (r GitCleanupResult) Failed() bool {
	return r.Worktree == CleanupFailed || r.LocalBranch == CleanupFailed || r.RemoteBranch == CleanupFailed
}

// RunTaskGit bounds Git and its subprocess group by the task lifetime.
func RunTaskGit(ctx context.Context, dir string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G702: fixed Git binary with structured arguments, no shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Cancel()
		}
	}()
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		uxlog.Log("[worktree] git interrupted dir=%q err=%v", dir, ctx.Err())
		return strings.TrimSpace(string(out)), -1, ctx.Err()
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return strings.TrimSpace(string(out)), code, err
}

// TaskGitCleanupBranch resolves the same legacy branch fallback used by cleanup.
func TaskGitCleanupBranch(worktree, branch string) string {
	if branch != "" && !strings.HasPrefix(branch, "argus/") {
		if worktree != "" {
			return "argus/" + filepath.Base(worktree)
		}
		return ""
	}
	return branch
}

// CleanupTaskGit removes a task's worktree and local/remote branches under the
// per-repo cleanup lock, then re-checks each result with git instead of
// trusting command exit status. Only argus-managed worktree paths and argus/*
// branches are touched.
func CleanupTaskGit(worktree, branch, repoDir string) GitCleanupResult {
	return CleanupTaskGitContext(context.Background(), worktree, branch, repoDir)
}

// CleanupTaskGitContext bounds cleanup subprocesses by the daemon lifetime.
func CleanupTaskGitContext(ctx context.Context, worktree, branch, repoDir string) GitCleanupResult {
	unlock, err := lockRepoContext(ctx, repoDir)
	if err != nil {
		uxlog.Log("[worktree] cleanup lock interrupted repo=%q err=%v", repoDir, err)
		return GitCleanupResult{Worktree: CleanupFailed, LocalBranch: CleanupNotApplicable, RemoteBranch: CleanupNotApplicable, Err: "cleanup lock interrupted"}
	}
	defer unlock()

	res := GitCleanupResult{Worktree: CleanupNotApplicable, LocalBranch: CleanupNotApplicable, RemoteBranch: CleanupNotApplicable}
	var errs []string
	fail := func(msg string) { errs = append(errs, msg) }

	dir := repoDir
	if dir == "" && worktree != "" {
		dir = filepath.Dir(worktree)
	}

	if worktree != "" {
		switch {
		case testGuard(worktree):
			res.Worktree = CleanupFailed
			fail("worktree path blocked during test")
		case ctx.Err() != nil:
			res.Worktree = CleanupFailed
			fail("cleanup interrupted")
		default:
			path, err := checkedTaskWorktree(ctx, worktree, repoDir)
			if errors.Is(err, os.ErrNotExist) {
				res.Worktree = CleanupAbsent
			} else if err != nil {
				res.Worktree = CleanupFailed
				fail(err.Error())
			} else {
				stopDevStackFor(path)
				if _, _, err := RunTaskGit(ctx, repoDir, "worktree", "remove", "--force", path); err != nil {
					res.Worktree = CleanupFailed
					fail("worktree removal failed: " + err.Error())
				} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					res.Worktree = CleanupFailed
					fail("worktree directory still exists")
				} else {
					res.Worktree = CleanupRemoved
				}
			}
		}
	}

	target := TaskGitCleanupBranch(worktree, branch)
	if target == "" || dir == "" || res.Worktree == CleanupFailed {
		res.Err = strings.Join(errs, "; ")
		return res
	}

	if _, _, err := RunTaskGit(ctx, dir, "worktree", "prune"); errors.Is(err, exec.ErrWaitDelay) {
		res.LocalBranch = CleanupFailed
		res.Err = "worktree prune output timeout"
		return res
	}

	branchExists := func() (bool, error) {
		_, code, err := RunTaskGit(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+target)
		if err == nil {
			return true, nil
		}
		if code == 1 {
			return false, nil
		}
		return false, err
	}
	if ok, err := branchExists(); err != nil {
		res.LocalBranch = CleanupFailed
		fail("local branch check failed: " + err.Error())
	} else if !ok {
		res.LocalBranch = CleanupAbsent
	} else {
		if out, _, derr := RunTaskGit(ctx, dir, "branch", "-D", target); derr != nil {
			uxlog.Log("[worktree] git branch -D %q failed in %q: %v: %s", target, dir, derr, out)
			if errors.Is(derr, exec.ErrWaitDelay) {
				res.LocalBranch = CleanupFailed
				fail("local branch output timeout")
			}
		}

		if res.LocalBranch != CleanupFailed {
			if still, err := branchExists(); err != nil || still {
				res.LocalBranch = CleanupFailed
				fail("local branch still exists")
			} else {
				res.LocalBranch = CleanupRemoved
			}
		}
	}

	if _, code, err := RunTaskGit(ctx, dir, "remote", "get-url", "origin"); err == nil {
		remoteExists := func() (bool, error) {
			_, code, err := RunTaskGit(ctx, dir, "ls-remote", "--exit-code", "--heads", "origin", target)
			if err == nil {
				return true, nil
			}
			if code == 2 {
				return false, nil
			}
			return false, err
		}
		if ok, err := remoteExists(); err != nil {
			res.RemoteBranch = CleanupFailed
			fail("remote branch check failed: " + err.Error())
		} else if !ok {
			res.RemoteBranch = CleanupAbsent
		} else {
			if out, _, derr := RunTaskGit(ctx, dir, "push", "origin", "--delete", target); derr != nil {
				uxlog.Log("[worktree] git push --delete %q failed in %q: %v: %s", target, dir, derr, out)
				if errors.Is(derr, context.DeadlineExceeded) || errors.Is(derr, context.Canceled) || errors.Is(derr, exec.ErrWaitDelay) {
					res.RemoteBranch = CleanupFailed
					fail("remote branch deletion interrupted: " + derr.Error())
					res.Err = strings.Join(errs, "; ")
					return res
				}
			}
			if still, err := remoteExists(); err != nil || still {
				res.RemoteBranch = CleanupFailed
				fail("remote branch still exists")
			} else {
				res.RemoteBranch = CleanupRemoved
			}
		}
	} else if code != 2 {
		res.RemoteBranch = CleanupFailed
		fail("origin lookup failed: " + err.Error())
	}

	res.Err = strings.Join(errs, "; ")
	return res
}
