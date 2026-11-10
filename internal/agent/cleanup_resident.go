package agent

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// RemoveWorktreeAndLocalBranch is RemoveWorktreeAndBranch minus the remote
// branch delete, for the resident's hera_delete_worker: everything it does is
// local and bounded, and it returns the repo dir and branch the caller should
// delete on origin later (DeleteRemoteBranchContext, run asynchronously with
// its own timeout). remoteBranch is "" when there is nothing to delete.
// RemoveWorktreeAndBranch itself is unchanged for every other caller.
func RemoveWorktreeAndLocalBranch(worktreePath, branch, repoDir string) (remoteRepoDir, remoteBranch string) {
	unlock := lockRepo(repoDir)
	defer unlock()
	if testGuard(worktreePath) {
		return "", ""
	}
	uxlog.Log("[worktree] RemoveWorktreeAndLocalBranch: path=%q branch=%q repoDir=%q", worktreePath, branch, repoDir)
	RemoveWorktree(worktreePath, repoDir)
	if branch == "" {
		uxlog.Log("[worktree] branch is empty, skipping branch cleanup")
		return "", ""
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
	DeleteBranch(dir, actualBranch)
	return dir, actualBranch
}

// DeleteRemoteBranchContext deletes branch on origin, bounded by ctx, and
// reports the outcome (DeleteRemoteBranch is best-effort and silent). It does
// not take the repo lock: a slow push must never block a later local removal
// in the same repo.
func DeleteRemoteBranchContext(ctx context.Context, repoDir, branch string) error {
	if branch == "" || repoDir == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "push", "origin", "--delete", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("git push origin --delete %s: %w", branch, ctxErr)
		}
		return fmt.Errorf("git push origin --delete %s: %w: %s", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}
