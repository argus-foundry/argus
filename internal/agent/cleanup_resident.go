package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// LockRepoContext acquires the per-repo cleanup lock (see repoLocks) and
// returns its unlock function, or ctx's error once ctx is done. A blank
// repoDir is a no-op lock.
func LockRepoContext(ctx context.Context, repoDir string) (unlock func(), err error) {
	return lockRepoContext(ctx, repoDir)
}

// TryLockRepo acquires the per-repo cleanup lock without waiting. ok is false
// when another cleanup holds it. A blank repoDir always succeeds (no lock).
// The resident's hera_delete_worker polls it on its own (fakeable) clock so
// the wait is bounded by the call's budget; lockRepo's callers are unchanged.
func TryLockRepo(repoDir string) (unlock func(), ok bool) {
	if repoDir == "" {
		return func() {}, true
	}
	v, _ := repoLocks.LoadOrStore(repoDir, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	if mu.TryLock() {
		return mu.Unlock, true
	}
	return nil, false
}

// RemoveWorktreeAndLocalBranch is RemoveWorktreeAndBranch minus the remote
// branch delete, for the resident's hera_delete_worker: everything it does is
// local and bounded, and it returns the repo dir and branch the caller should
// delete on origin later (DeleteRemoteBranchContext, run asynchronously with
// its own timeout) plus leaseTip, the commit that delete must still find on
// origin (see RemoveWorktreeAndLocalBranchLocked). remoteBranch is "" when
// there is nothing to delete. RemoveWorktreeAndBranch itself is unchanged for
// every other caller.
func RemoveWorktreeAndLocalBranch(worktreePath, branch, repoDir string) (remoteRepoDir, remoteBranch, leaseTip string) {
	unlock := lockRepo(repoDir)
	defer unlock()
	return RemoveWorktreeAndLocalBranchLocked(worktreePath, branch, repoDir)
}

// RemoveWorktreeAndLocalBranchLocked is RemoveWorktreeAndLocalBranch for a
// caller that already holds repoDir's cleanup lock (LockRepoContext or
// TryLockRepo): the resident delete takes it before its first change, with a
// bounded wait, so nothing after the stop can block on it.
//
// leaseTip is captured BEFORE the local branch is deleted: the origin
// remote-tracking ref (what this repo last saw on origin) when there is one,
// else the local branch tip, "" when neither resolves. The asynchronous
// origin delete leases on it, so a branch of the same name that a new worker
// pushed in the meantime is left alone.
func RemoveWorktreeAndLocalBranchLocked(worktreePath, branch, repoDir string) (remoteRepoDir, remoteBranch, leaseTip string) {
	if testGuard(worktreePath) {
		return "", "", ""
	}
	uxlog.Log("[worktree] RemoveWorktreeAndLocalBranch: path=%q branch=%q repoDir=%q", worktreePath, branch, repoDir)
	RemoveWorktree(worktreePath, repoDir)
	if branch == "" {
		uxlog.Log("[worktree] branch is empty, skipping branch cleanup")
		return "", "", ""
	}
	dir := repoDir
	if dir == "" {
		dir = filepath.Dir(worktreePath)
	}
	pruneWorktrees(dir)
	// Same base-branch inference as RemoveWorktreeAndBranch.
	actualBranch := branch
	if !strings.HasPrefix(branch, "argus/") {
		actualBranch = "argus/" + filepath.Base(worktreePath)
		uxlog.Log("[worktree] stored branch %q is not argus/*, trying inferred branch %q", branch, actualBranch)
	}
	leaseTip = revParseCommit(dir, "refs/remotes/origin/"+actualBranch)
	if leaseTip == "" {
		leaseTip = revParseCommit(dir, "refs/heads/"+actualBranch)
	}
	DeleteBranch(dir, actualBranch)
	return dir, actualBranch, leaseTip
}

// revParseCommit resolves ref to a commit id in dir, "" when it does not.
func revParseCommit(dir, ref string) string {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ErrRemoteBranchChanged reports that DeleteRemoteBranchContext's lease
// failed and origin still has the branch at a different commit (a new worker
// of the same name pushed it, or it moved server-side), so it was left in
// place. Not a failure to retry; a fetch-and-retry with an ancestry-checked
// lease is a named follow-up (gotchas/resident.md).
var ErrRemoteBranchChanged = errors.New("remote branch changed on origin; left in place")

// ErrRemoteBranchGone reports that origin no longer has the branch at all
// (typically merged and auto-deleted): nothing to do.
var ErrRemoteBranchGone = errors.New("remote branch already gone")

// DeleteRemoteBranchContext deletes branch on origin, bounded by ctx, and
// reports the outcome (DeleteRemoteBranch is best-effort and silent). The
// delete is leased on expectTip (git push --force-with-lease): when origin's
// branch no longer points there it is left alone and ErrRemoteBranchChanged
// is returned. An empty expectTip refuses (an unleased delete could remove a
// re-created branch). It does not take the repo lock: a slow push must never
// block a later local removal in the same repo.
func DeleteRemoteBranchContext(ctx context.Context, repoDir, branch, expectTip string) error {
	if branch == "" || repoDir == "" {
		return nil
	}
	if expectTip == "" {
		return fmt.Errorf("delete origin branch %s: no expected tip to lease on; left in place", branch)
	}
	ref := "refs/heads/" + branch
	cmd := exec.CommandContext(ctx, "git", "push", "--force-with-lease="+ref+":"+expectTip, "origin", ":"+ref)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("git push origin --delete %s: %w", branch, ctxErr)
		}
		msg := strings.TrimSpace(string(out))
		// Current git reports a lease mismatch, including a branch already gone
		// from origin, as "stale info"; some versions report the gone case as
		// "remote ref does not exist". Either way ls-remote classifies it.
		if strings.Contains(msg, "stale info") || strings.Contains(msg, "remote ref does not exist") {
			present, lsErr := remoteBranchExists(ctx, repoDir, ref)
			switch {
			case lsErr == nil && !present:
				return fmt.Errorf("%w: origin has no %s", ErrRemoteBranchGone, branch)
			case lsErr != nil:
				return fmt.Errorf("%w: origin %s is no longer at %s (could not confirm it still exists: %w)", ErrRemoteBranchChanged, branch, expectTip, lsErr)
			}
			return fmt.Errorf("%w: origin %s is no longer at %s", ErrRemoteBranchChanged, branch, expectTip)
		}
		return fmt.Errorf("git push origin --delete %s: %w: %s", branch, err, msg)
	}
	return nil
}

// remoteBranchExists asks origin whether ref exists (git ls-remote), bounded
// by ctx. It only classifies a failed lease for logging; it never decides a
// delete.
func remoteBranchExists(ctx context.Context, repoDir, ref string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "origin", ref)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 2:
		return false, nil // --exit-code: no matching ref
	default:
		return false, err
	}
}
