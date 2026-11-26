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
	byBaseBranch := make(map[string]*model.Task, len(allTasks))
	for _, t := range allTasks {
		if t.BaseBranch != "" {
			byBaseBranch[t.BaseBranch] = t
		}
	}
	tipVerdicts := make(map[string]mergesafety.Verdict)

	for _, t := range candidates {
		reconcileHeraReclaim(ctx, d, runner, cfg, t.ID, byBaseBranch, tipVerdicts, &sum)
	}

	uxlog.Log("[hera] reclaim sweep: candidates=%d worktrees_retried=%d stacked_branches_deleted=%d pruned=%d skipped_live_session=%d",
		sum.Candidates, sum.WorktreesRetried, sum.StackedBranchesDeleted, sum.Pruned, sum.SkippedLiveSession)

	return sum, nil
}

func reconcileHeraReclaim(ctx context.Context, d *db.DB, runner SessionChecker, cfg config.Config, id string, byBaseBranch map[string]*model.Task, tipVerdicts map[string]mergesafety.Verdict, sum *Summary) {
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

	repoDir := agent.ResolveDir(t, cfg)
	if t.Worktree != "" && agent.DirExists(t.Worktree) {
		agent.RemoveWorktreeAndBranch(t.Worktree, t.Branch, repoDir)
		sum.WorktreesRetried++
		uxlog.Log("[hera] reclaim sweep: retried worktree+branch removal task=%s", id)
	}

	sum.StackedBranchesDeleted += reconcileStackedBranches(ctx, d, cfg, byBaseBranch, t, tipVerdicts)
	if t.Archived && t.Worktree == "" && t.Branch != "" && repoDir != "" {
		agent.DeleteBranch(repoDir, t.Branch)
		agent.DeleteRemoteBranch(repoDir, t.Branch)
	}
	pruned, _, err := d.PruneTasks([]string{id})
	if err != nil {
		uxlog.Log("[hera] reclaim sweep: prune failed task=%s: %v", id, err)
		return
	}
	if len(pruned) != 0 {
		sum.Pruned++
		uxlog.Log("[hera] reclaim sweep: pruned task row %s", id)
	}
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
func reconcileStackedBranches(ctx context.Context, d *db.DB, cfg config.Config, byBaseBranch map[string]*model.Task, x *model.Task, tipVerdicts map[string]mergesafety.Verdict) int {
	chain, tip, ok := walkStackForward(x, byBaseBranch)
	if !ok || len(chain) == 0 {
		return 0
	}

	repoDir, repoSlug, defaultRef, defaultShort := projectMergeSafetyContext(ctx, cfg, x)
	if repoDir == "" {
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
		if !branchExistsOnOrigin(repoDir, y.Branch) {
			continue
		}

		agent.DeleteBranch(repoDir, y.Branch)
		agent.DeleteRemoteBranch(repoDir, y.Branch)
		deleted++
		uxlog.Log("[hera] reclaim sweep: deleted stacked branch %q (task=%s) confirmed via %s", y.Branch, y.ID, v.Tier)
	}
	return deleted
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
// ref under origin — a purely local check (git for-each-ref against the
// repo's own refs/remotes/, no fetch), mirroring Tier A's own no-fetch
// staleness tradeoff rather than a live network round trip.
func branchExistsOnOrigin(repoDir, branch string) bool {
	target := "origin/" + branch
	return slices.Contains(gitutil.ListRemoteBranches(repoDir), target)
}
