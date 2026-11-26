package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	dir, actualBranch, leaseTip := WorktreeRemoteBranchLease(worktreePath, branch, repoDir)
	RemoveWorktree(worktreePath, repoDir)
	if actualBranch == "" {
		uxlog.Log("[worktree] branch is empty, skipping branch cleanup")
		return "", "", ""
	}
	pruneWorktrees(dir)
	DeleteBranch(dir, actualBranch)
	return dir, actualBranch, leaseTip
}

// WorktreeRemoteBranchLease captures the remote-tracking or local tip before
// resident deletion can defer worktree removal to a later owner.
func WorktreeRemoteBranchLease(worktreePath, branch, repoDir string) (remoteRepoDir, remoteBranch, leaseTip string) {
	if branch == "" || (worktreePath == "" && repoDir == "") {
		return "", "", ""
	}
	dir := repoDir
	if dir == "" {
		dir = ResidentCleanupRepository(context.Background(), worktreePath, "")
		if dir == "" {
			return "", "", ""
		}
	}
	// Same base-branch inference as RemoveWorktreeAndBranch.
	actualBranch := branch
	if worktreePath != "" && !strings.HasPrefix(branch, "argus/") {
		actualBranch = "argus/" + filepath.Base(worktreePath)
		uxlog.Log("[worktree] stored branch %q is not argus/*, trying inferred branch %q", branch, actualBranch)
	}
	leaseTip = revParseCommit(dir, "refs/remotes/origin/"+actualBranch)
	if leaseTip == "" {
		leaseTip = revParseCommit(dir, "refs/heads/"+actualBranch)
	}
	return dir, actualBranch, leaseTip
}

// ResidentCleanupRepository resolves a surviving linked worktree when its
// configured repository is unavailable. The common Git directory survives
// removal of the linked checkout.
func ResidentCleanupRepository(ctx context.Context, worktree, repoDir string) string {
	var linkedRepo string
	if worktree != "" {
		if _, err := os.Lstat(filepath.Join(worktree, ".git")); err == nil {
			linkedRepo, _ = TaskGitRepositoryID(ctx, worktree)
		}
	}
	if repoDir != "" {
		if configured, err := TaskGitRepositoryID(ctx, repoDir); err == nil && (linkedRepo == "" || SamePath(linkedRepo, configured)) {
			return repoDir
		}
	}
	if linkedRepo == "" {
		return ""
	}
	if filepath.Base(linkedRepo) == ".git" {
		return filepath.Dir(linkedRepo)
	}
	return linkedRepo
}

// CleanupResidentLocalGit checks registration before deleting, and never
// substitutes recursive filesystem deletion for a refusal from Git.
func CleanupResidentLocalGit(ctx context.Context, worktree, branch, repoDir string, repoLocked bool) (bool, error) {
	if worktree == "" && branch == "" {
		return false, nil
	}
	if !repoLocked {
		unlock, err := lockRepoContext(ctx, repoDir)
		if err != nil {
			return false, err
		}
		defer unlock()
	}
	removed := false
	if worktree != "" {
		if testGuard(worktree) {
			return false, errors.New("worktree path blocked during test")
		}
		path, err := checkedTaskWorktree(ctx, worktree, repoDir)
		if err == nil {
			stopDevStackFor(path)
			stopBrowsersFor(path)
			if _, _, err := RunTaskGit(ctx, repoDir, "worktree", "remove", "--force", path); err != nil {
				return false, fmt.Errorf("remove resident worktree: %w", err)
			}
			removed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if branch != "" && repoDir != "" {
		if worktree != "" {
			if _, _, err := RunTaskGit(ctx, repoDir, "worktree", "prune"); err != nil {
				return false, err
			}
		}
		if out, _, err := RunTaskGit(ctx, repoDir, "branch", "-D", "--", branch); err != nil {
			uxlog.Log("[worktree] resident local delete failed repo=%q branch=%q err=%v output=%q", repoDir, branch, err, out)
		}
	}
	return removed, VerifyResidentLocalRemoval(ctx, worktree, branch, repoDir)
}

// VerifyResidentLocalRemoval checks the actual filesystem and ref state. The
// best-effort removal helpers may log a failure without returning one.
func VerifyResidentLocalRemoval(ctx context.Context, worktree, branch, repoDir string) error {
	if worktree != "" {
		if _, err := os.Stat(worktree); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return fmt.Errorf("verify worktree removal: %w", err)
			}
			return errors.New("resident worktree remains after cleanup")
		}
	}
	if branch == "" {
		return nil
	}
	if repoDir == "" {
		return errors.New("repository unavailable for local branch verification")
	}
	_, code, err := RunTaskGit(ctx, repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return errors.New("resident branch remains after cleanup")
	}
	if code != 1 {
		return fmt.Errorf("verify local branch removal: %w", err)
	}
	return nil
}

// DeleteStackedBranchContext bounds Git work done while task admission is held.
func DeleteStackedBranchContext(ctx context.Context, repoDir, branch string) {
	if out, _, err := RunTaskGit(ctx, repoDir, "branch", "-D", "--", branch); err != nil {
		uxlog.Log("[worktree] stacked local delete failed repo=%q branch=%q err=%v output=%q", repoDir, branch, err, out)
	}
	if out, _, err := RunTaskGit(ctx, repoDir, "push", "origin", ":refs/heads/"+branch); err != nil {
		uxlog.Log("[worktree] stacked origin delete failed repo=%q branch=%q err=%v output=%q", repoDir, branch, err, out)
	}
}

// revParseCommit resolves ref to a commit id in dir, "" when it does not.
func revParseCommit(dir, ref string) string {
	out, _, err := RunTaskGit(context.Background(), dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return out
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
	msg, _, err := RunTaskGit(ctx, repoDir, "push", "--force-with-lease="+ref+":"+expectTip, "origin", ":"+ref)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("git push origin --delete %s: %w", branch, ctxErr)
		}
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
	_, code, err := RunTaskGit(ctx, repoDir, "ls-remote", "--exit-code", "origin", ref)
	switch {
	case err == nil:
		return true, nil
	case code == 2:
		return false, nil // --exit-code: no matching ref
	default:
		return false, err
	}
}
