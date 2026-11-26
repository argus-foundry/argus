// reclaim_sweep.go is Stage 3 of fix-hera-nuke-cleanup: a reconciliation
// sweep that durably finishes cascade-nuke cleanup left behind by a daemon
// crash or restart mid-teardown. heraReclaimAndArchiveTask
// (internal/tui/heraactions.go) reclaims a nuked task's worktree+branch in a
// backgrounded goroutine and then prunes the row once that settles — both of
// which die with the process if the daemon exits first. ReconcileHeraReclaims
// is the sibling to heraadopt.ReconcileBindings (same idempotent,
// safe-to-rerun, run-on-every-boot contract): it re-discovers exactly the
// work an interrupted nuke left unfinished and retries it.
package hera

import (
	"context"
	"errors"
	"slices"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// SessionChecker is the narrow runner surface ReconcileHeraReclaims needs to
// tell whether a candidate task's reclaim-triggered session stop has
// settled yet. In-process and RPC owners also implement
// agent.HeraReclaimSessionOwner to certify that there is no pending start.
type SessionChecker interface {
	HasSession(taskID string) bool
}

// Summary reports what one ReconcileHeraReclaims pass did, for the caller
// (daemon startup / periodic ticker, Stage 6) to log.
type Summary struct {
	// Candidates is the size of the candidate set this pass considered.
	Candidates int
	// WorktreesRetried counts candidates whose worktree directory still
	// existed on disk, so its worktree + own branch removal was retried.
	WorktreesRetried int
	// StackedBranchesDeleted counts additional stacked branches (belonging
	// to other tasks in a candidate's base_branch chain) deleted via the
	// best-effort Tier D re-discovery — see reconcileStackedBranches.
	StackedBranchesDeleted int
	// Pruned counts candidate rows deleted this pass.
	Pruned int
	// SkippedLiveSession counts candidates whose session or exit relay is
	// still unsettled, or whose lifecycle is being changed concurrently.
	SkippedLiveSession int
}

// ReconcileHeraReclaims durably finishes cascade-nuke cleanup for every task
// left behind by an interrupted nuke. The candidate set is every task with a
// hera_bindings row ended for the nuke path's own reason
// (db.HeraEndReasonUserDeleted) that holds no live binding
// (db.HeraNukeReclaimCandidates — the same live-binding guard PruneTasks
// re-verifies at delete time). Wait for the owner to confirm retirement and
// for any durable stop intent to settle before retrying worktree and branch
// cleanup or pruning the task.
//
// Idempotent and safe to call repeatedly, including on every daemon boot: a
// candidate whose cleanup already fully completed (worktree gone, nothing
// left to classify in its stack) performs no filesystem or network work at
// all before being pruned.
func ReconcileHeraReclaims(d *db.DB, runner SessionChecker) (Summary, error) {
	var sum Summary

	candidates, err := d.HeraNukeReclaimCandidates()
	if err != nil {
		return sum, err
	}
	sum.Candidates = len(candidates)
	if len(candidates) == 0 {
		return sum, nil
	}

	cfg := d.Config()
	ctx := context.Background()

	allTasks, err := d.Tasks()
	if err != nil {
		return sum, err
	}
	for _, t := range candidates {
		reconcileHeraReclaim(ctx, d, runner, cfg, t.ID, allTasks, &sum)
	}

	uxlog.Log("[hera] reclaim sweep: candidates=%d worktrees_retried=%d stacked_branches_deleted=%d pruned=%d skipped_live_session=%d",
		sum.Candidates, sum.WorktreesRetried, sum.StackedBranchesDeleted, sum.Pruned, sum.SkippedLiveSession)

	return sum, nil
}

func reconcileHeraReclaim(ctx context.Context, d *db.DB, runner SessionChecker, cfg config.Config, id string, allTasks []*model.Task, sum *Summary) {
	unlock, acquired, err := d.TryLockSessionMutation(id)
	if err != nil || !acquired {
		sum.SkippedLiveSession++
		uxlog.Log("[hera] reclaim sweep: session lock unavailable task=%s err=%v", id, err)
		return
	}
	defer unlock()
	releaseReclaim, acquired, err := d.TryLockHeraReclaim(id)
	if err != nil || !acquired {
		uxlog.Log("[hera] reclaim sweep: binding admission busy task=%s err=%v", id, err)
		return
	}
	defer releaseReclaim()

	pendingNuke, err := d.HasUnsettledHeraNuke(id)
	if err != nil || !pendingNuke {
		uxlog.Log("[hera] reclaim sweep: candidate settled task=%s err=%v", id, err)
		return
	}
	t, err := d.Get(id)
	if err != nil {
		uxlog.Log("[hera] reclaim sweep: task reload failed task=%s err=%v", id, err)
		return
	}
	liveBindings, err := d.ListHeraLiveBindingsByTask(id)
	if err != nil || len(liveBindings) != 0 {
		uxlog.Log("[hera] reclaim sweep: binding guard task=%s live=%d err=%v", id, len(liveBindings), err)
		return
	}
	if pending, err := d.GraduationPendingErr(id); err != nil || pending {
		uxlog.Log("[resident] reclaim sweep: skipped task=%s graduation_pending=%t err=%v", id, pending, err)
		return
	}
	pending, err := d.HeraReclaimStopPending(id)
	if err != nil || pending {
		sum.SkippedLiveSession++
		uxlog.Log("[hera] reclaim sweep: exit relay unsettled task=%s pending=%t err=%v", id, pending, err)
		return
	}
	if owner, ok := runner.(agent.HeraReclaimSessionOwner); ok {
		err = owner.CheckReclaimSessionRetired(id)
	} else if runner.HasSession(id) {
		err = agent.ErrSessionStarting
	}
	if err != nil {
		sum.SkippedLiveSession++
		uxlog.Log("[hera] reclaim sweep: retirement not confirmed task=%s err=%v", id, err)
		return
	}
	lease, err := d.HeraResidentReclaimLease(id)
	if err != nil {
		uxlog.Log("[resident] reclaim sweep: unreadable lease task=%s err=%v", id, err)
		return
	}

	repoDir := agent.ResolveDir(t, cfg)
	if lease != nil {
		removed, err := settleResidentLocalReclaim(ctx, d, cfg, t, lease, false)
		if err != nil {
			uxlog.Log("[resident] reclaim sweep: local ownership unavailable task=%s err=%v", id, err)
			return
		}
		if removed {
			sum.WorktreesRetried++
		}
	} else if t.Worktree != "" && agent.DirExists(t.Worktree) {
		agent.RemoveWorktreeAndBranch(t.Worktree, t.Branch, repoDir)
		sum.WorktreesRetried++
		uxlog.Log("[hera] reclaim sweep: retried worktree+branch removal task=%s", id)
	}

	byBase := StackTasksInRepository(ctx, cfg, repoDir, allTasks)
	sum.StackedBranchesDeleted += reconcileStackedBranches(ctx, d, cfg, byBase, t, map[string]mergesafety.Verdict{}, lease != nil)
	if lease != nil {
		if err := residentRemoteBranchDeleteOwned(ctx, d, ResidentDeleteOptions{}, id, lease); err != nil {
			uxlog.Log("[resident] reclaim sweep: remote ownership unavailable task=%s err=%v", id, err)
			return
		}
	} else if t.Archived && t.Worktree == "" && t.Branch != "" && repoDir != "" {
		agent.DeleteBranch(repoDir, t.Branch)
		agent.DeleteRemoteBranch(repoDir, t.Branch)
	}
	pruned, _, err := d.PruneHeraReclaimedTask(id)
	if err != nil {
		uxlog.Log("[hera] reclaim sweep: prune failed task=%s: %v", id, err)
		return
	}
	if len(pruned) != 0 {
		sum.Pruned++
		uxlog.Log("[hera] reclaim sweep: pruned task row %s", id)
	}
}

// StackTasksInRepository keeps identically named branches in another Git
// repository out of the evidence and admission checks for this stack.
func StackTasksInRepository(ctx context.Context, cfg config.Config, repoDir string, tasks []*model.Task) map[string]*model.Task {
	root, err := agent.TaskGitRepositoryID(ctx, repoDir)
	if err != nil || root == "" {
		return nil
	}
	identities := map[string]string{repoDir: root}
	byBase := make(map[string]*model.Task)
	for _, task := range tasks {
		if task.BaseBranch == "" {
			continue
		}
		repo := agent.ResolveDir(task, cfg)
		identity, cached := identities[repo]
		if !cached {
			identity, _ = agent.TaskGitRepositoryID(ctx, repo)
			identities[repo] = identity
		}
		if identity != "" && agent.SamePath(identity, root) {
			byBase[task.BaseBranch] = task
		}
	}
	return byBase
}

// settleResidentLocalReclaim fences worktree creation before checking for a
// replacement. It records the completed local step before any Git network I/O.
func settleResidentLocalReclaim(ctx context.Context, d *db.DB, cfg config.Config, task *model.Task, lease *db.HeraReclaimLease, repoLocked bool) (bool, error) {
	if lease.LocalDone {
		return false, nil
	}
	worktree := task.Worktree
	if !agent.IsWorktreeSubdir(worktree) {
		worktree = ""
	}
	branch := lease.Branch
	if branch == "" {
		branch = agent.TaskGitCleanupBranch(worktree, task.Branch)
	}
	if worktree == "" && branch == "" {
		lease.LocalDone = true
		return false, d.SetResidentReclaimLease(task.ID, lease)
	}
	repoDir := lease.RepoDir
	if repoDir == "" {
		return false, errors.New("original reclaim repository was unavailable at nuke time")
	}
	unlock, acquired, err := agent.TryLockTaskGitOwnership(ctx, repoDir)
	if err != nil {
		return false, err
	}
	if !acquired {
		return false, db.ErrHeraReclaimInProgress
	}
	defer unlock()
	if err := verifyReclaimRepository(ctx, lease); err != nil {
		return false, err
	}
	worktree, branch, err = residentReclaimTargets(ctx, d, cfg, task.ID, repoDir, worktree, branch)
	if err != nil {
		return false, err
	}
	if branch == "" {
		lease.Tip = ""
	}
	removed, err := agent.CleanupResidentLocalGit(ctx, worktree, branch, repoDir, repoLocked)
	if err != nil {
		return false, err
	}
	lease.LocalDone = true
	return removed, d.SetResidentReclaimLease(task.ID, lease)
}

func verifyReclaimRepository(ctx context.Context, lease *db.HeraReclaimLease) error {
	if lease.RepoID == "" {
		return nil
	}
	current, err := agent.TaskGitRepositoryID(ctx, lease.RepoDir)
	if err != nil {
		return err
	}
	if !agent.SamePath(current, lease.RepoID) {
		return errors.New("captured reclaim repository changed")
	}
	return nil
}

// residentReclaimTargets is called with Git ownership fenced across creation.
func residentReclaimTargets(ctx context.Context, d *db.DB, cfg config.Config, taskID, repoDir, worktree, branch string) (string, string, error) {
	tasks, err := d.TasksChecked()
	if err != nil {
		return "", "", err
	}
	for _, other := range tasks {
		if other.ID == taskID {
			continue
		}
		if worktree != "" && agent.SamePath(other.Worktree, worktree) {
			return "", "", nil
		}
		if agent.TaskReferencesGitBranch(other, branch) {
			targetID, err := agent.TaskGitRepositoryID(ctx, repoDir)
			if err != nil {
				return "", "", err
			}
			otherID, err := agent.TaskGitRepositoryID(ctx, agent.ResolveDir(other, cfg))
			if err != nil {
				return "", "", err
			}
			if otherID == "" || agent.SamePath(targetID, otherID) {
				branch = ""
			}
		}
	}
	return worktree, branch, nil
}

// walkStackForward walks a base_branch stack forward from x exactly like
// mergesafety.ResolveStackTip (same cycle guard, same "no descendant found"
// terminal condition), except it ALSO returns every task visited along the
// way — INCLUDING x, EXCLUDING the resolved tip — so the caller can consider
// each one for stacked-branch deletion once the tip itself classifies safe.
// ok is false on the same cycle condition ResolveStackTip fails closed on;
// an empty chain (tip == x) means x has no descendant in this scope, exactly
// mirroring ClassifyStackInferred's own "no descendant found" case.
func walkStackForward(x *model.Task, byBaseBranch map[string]*model.Task) (chain []*model.Task, tip *model.Task, ok bool) {
	seen := make(map[string]bool)
	cur := x
	for {
		if cur.Branch != "" {
			if seen[cur.Branch] {
				return nil, nil, false
			}
			seen[cur.Branch] = true
		}
		next, exists := byBaseBranch[cur.Branch]
		if !exists || next == nil {
			return chain, cur, true
		}
		chain = append(chain, cur)
		cur = next
	}
}

// reconcileStackedBranches best-effort re-discovers and retries deleting any
// additional stacked branch in candidate x's base_branch chain — a task
// branched off x (or off a descendant of x) that never got a standalone PR
// of its own, so only the chain's resolved tip classifies safe via ordinary
// Tier A/B (fix-hera-nuke-cleanup design.md Decision D5: the operator's
// exclusion choice at nuke time is the one piece of durable state this
// sweep can't re-derive, so everything else — INCLUDING which branches are
// even candidates — is re-computed fresh every run). A branch the operator
// recorded excluded (db.ExcludedCleanupBranches) is never touched, however
// its chain classifies.
//
// This is secondary to the core worktree-retry/prune sweep above: any error
// here is logged and skipped, never surfaced to the caller, and a resolution
// failure (no project, no repo, tip not confirmed safe) simply results in
// zero deletions for this candidate.
func reconcileStackedBranches(ctx context.Context, d *db.DB, cfg config.Config, byBaseBranch map[string]*model.Task, x *model.Task, tipVerdicts map[string]mergesafety.Verdict, leasedRoot bool) int {
	chain, tip, ok := walkStackForward(x, byBaseBranch)
	if !ok || len(chain) == 0 {
		return 0
	}

	repoDir, repoSlug, defaultRef, defaultShort := projectMergeSafetyContext(ctx, cfg, x)
	if repoDir == "" {
		return 0
	}
	repoID, err := agent.TaskGitRepositoryID(ctx, repoDir)
	if err != nil || repoID == "" {
		return 0
	}

	tipVerdict, cached := tipVerdicts[tip.Branch]
	if !cached {
		v, err := mergesafety.Classify(ctx, mergesafety.Params{
			RepoDir:       repoDir,
			RepoSlug:      repoSlug,
			Branch:        tip.Branch,
			DefaultRef:    defaultRef,
			DefaultShort:  defaultShort,
			TaskCreatedAt: tip.CreatedAt,
		})
		if err != nil {
			uxlog.Log("[hera] reclaim sweep: stack tip classify failed task=%s tip=%s: %v", x.ID, tip.ID, err)
			return 0
		}
		tipVerdict = v
		tipVerdicts[tip.Branch] = v
	}
	if !tipVerdict.Safe {
		return 0
	}

	deleted := 0
	for _, y := range chain {
		if y.ID == x.ID && leasedRoot {
			continue
		}
		excluded, err := d.ExcludedCleanupBranches(y.ID)
		if err != nil {
			uxlog.Log("[hera] reclaim sweep: read excluded branches failed task=%s: %v", y.ID, err)
			continue
		}
		if slices.Contains(excluded, y.Branch) {
			continue
		}

		v, err := mergesafety.ClassifyStackInferred(ctx, mergesafety.StackParams{
			Task:         y,
			ByBaseBranch: byBaseBranch,
			RepoDir:      repoDir,
			RepoSlug:     repoSlug,
			DefaultRef:   defaultRef,
			DefaultShort: defaultShort,
		}, tipVerdicts)
		if err != nil {
			uxlog.Log("[hera] reclaim sweep: stack-inferred classify failed task=%s: %v", y.ID, err)
			continue
		}
		if !v.Safe {
			continue
		}
		if !branchExistsOnOrigin(ctx, repoDir, y.Branch) || !deleteStackedTaskBranch(ctx, d, y.ID, repoDir, repoID, y.Branch, y.ID == x.ID) {
			continue
		}

		deleted++
		uxlog.Log("[hera] reclaim sweep: deleted stacked branch %q (task=%s) confirmed via %s", y.Branch, y.ID, v.Tier)
	}
	return deleted
}

// DeleteStackedTaskBranch revalidates a confirmed stack entry against task
// creation and reclaim before deleting its branch.
func DeleteStackedTaskBranch(ctx context.Context, d *db.DB, taskID, repoDir, repoID, branch string) bool {
	return deleteStackedTaskBranch(ctx, d, taskID, repoDir, repoID, branch, false)
}

func deleteStackedTaskBranch(ctx context.Context, d *db.DB, taskID, repoDir, repoID, branch string, admissionHeld bool) bool {
	if !admissionHeld {
		unlock, err := d.TryLockTaskOutsideReclaim(taskID)
		if err != nil {
			uxlog.Log("[hera] stacked branch cleanup deferred task=%s err=%v", taskID, err)
			return false
		}
		defer unlock()
	}
	unlock, acquired, err := agent.TryLockTaskGitOwnership(ctx, repoDir)
	if err != nil || !acquired {
		uxlog.Log("[hera] stacked branch ownership unavailable task=%s err=%v", taskID, err)
		return false
	}
	defer unlock()
	current, err := d.Get(taskID)
	if err != nil || current.Branch != branch {
		uxlog.Log("[hera] stacked branch assignment changed task=%s err=%v", taskID, err)
		return false
	}
	root, err := agent.TaskGitRepositoryID(ctx, repoDir)
	if err != nil || root == "" || !agent.SamePath(root, repoID) {
		return false
	}
	cfg := d.Config()
	currentRepo, err := agent.TaskGitRepositoryID(ctx, agent.ResolveDir(current, cfg))
	if err != nil || !agent.SamePath(root, currentRepo) {
		return false
	}
	tasks, err := d.TasksChecked()
	if err != nil {
		return false
	}
	for _, other := range tasks {
		if other.ID == taskID || other.Branch != branch {
			continue
		}
		otherRepo, err := agent.TaskGitRepositoryID(ctx, agent.ResolveDir(other, cfg))
		if err != nil || otherRepo == "" || agent.SamePath(root, otherRepo) {
			uxlog.Log("[hera] stacked branch belongs to another task task=%s other=%s", taskID, other.ID)
			return false
		}
	}
	agent.DeleteStackedBranchContext(ctx, repoDir, branch)
	return true
}

// projectMergeSafetyContext resolves the shared, project-level
// mergesafety.Params/StackParams fields for task t's project — the same
// resolution steps internal/api/cleanup_candidates.go's cleanupCandidateFor
// performs, reused here rather than reimplemented. A task whose project no
// longer exists in config (or has an empty path) returns an empty repoDir;
// callers treat that as unresolvable, per Classify's own "no repo
// resolvable" fail-closed handling.
func projectMergeSafetyContext(ctx context.Context, cfg config.Config, t *model.Task) (repoDir, repoSlug, defaultRef, defaultShort string) {
	repoDir = agent.ResolveDir(t, cfg)
	if repoDir == "" {
		return "", "", "", ""
	}
	if slug, ok := gitutil.ResolveDefaultRepo(ctx, repoDir); ok {
		repoSlug = slug
	}
	proj := cfg.Projects[t.Project]
	if short, ref, err := gitutil.ResolveDefaultBranch(repoDir, proj.Branch); err == nil {
		defaultShort, defaultRef = short, ref
	}
	return repoDir, repoSlug, defaultRef, defaultShort
}

// branchExistsOnOrigin reports whether branch still has a remote-tracking
// ref under origin. This is a local check with no network fetch.
func branchExistsOnOrigin(ctx context.Context, repoDir, branch string) bool {
	_, _, err := agent.RunTaskGit(ctx, repoDir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}
