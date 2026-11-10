package hera

import (
	"context"
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// NukeResidentWorker is the resident's hera_delete_worker variant of
// NukeSingleRoleAs (actor = resident). It finishes the ROLE-SCOPED work
// synchronously — nuke the role, archive its task, stop that task's session,
// remove that task's own worktree and branch, prune its row once no session
// remains — so the tool result can be returned without waiting on the
// fleet-wide ReconcileHeraReclaims, which the caller runs asynchronously.
// The returned followUp re-discovers stacked branches off the deleted task's
// branch (network classification, the one per-task step the fleet sweep can
// no longer reach once the row is pruned); run it in the background too.
//
// Like NukeSingleRoleAs it never touches the resident itself or a graduate
// awaiting its restart, and preserves a task bound to another role.
func NukeResidentWorker(d *db.DB, runner NukeRunner, r *heramodel.RoleView) (followUp func(), err error) {
	noop := func() {}
	if r == nil || r.TaskID == "" {
		return noop, errors.New("no task to delete")
	}
	t, err := d.Get(r.TaskID)
	if err != nil {
		return noop, fmt.Errorf("load task: %w", err)
	}
	if t.IsResident() {
		return noop, errors.New("resident delete must never target itself")
	}
	// CheckNukeTask surfaces the real error for an unreadable marker instead of
	// reporting it as a pending graduation.
	if err := CheckNukeTask(d, t.ID); err != nil {
		uxlog.Log("[resident] delete role %d skipped task=%s: %v", r.RoleID, t.ID, err)
		return noop, err
	}
	reclaim := false
	if r.Live {
		live, lErr := d.ListHeraLiveBindingsByTask(t.ID)
		reclaim = lErr == nil && len(live) == 1 && live[0].RoleID == r.RoleID
	}
	if err := nukeRoleRow(d, r.RoleID, db.HeraActorResident); err != nil {
		return noop, fmt.Errorf("nuke role %d: %w", r.RoleID, err)
	}
	if !reclaim {
		uxlog.Log("[resident] delete role %d: task=%s preserved (bound elsewhere)", r.RoleID, t.ID)
		return noop, nil
	}
	archiveNukedTask(d, t.ID)
	reclaimResidentWorkerTask(d, runner, t)
	return func() { residentWorkerStackFollowUp(d, t) }, nil
}

// reclaimResidentWorkerTask is ReconcileHeraReclaims' per-candidate body for
// exactly one task, minus the stacked-branch re-discovery (see followUp).
func reclaimResidentWorkerTask(d *db.DB, runner NukeRunner, t *model.Task) {
	if runner != nil && runner.HasSession(t.ID) {
		if err := runner.Stop(t.ID); err != nil {
			uxlog.Log("[resident] delete: stop session failed task=%s: %v", t.ID, err)
		}
	}
	if t.Worktree != "" && agent.DirExists(t.Worktree) {
		agent.RemoveWorktreeAndBranch(t.Worktree, t.Branch, agent.ResolveDir(t, d.Config()))
		uxlog.Log("[resident] delete: removed worktree+branch task=%s", t.ID)
	}
	if runner != nil && runner.HasSession(t.ID) {
		uxlog.Log("[resident] delete: prune deferred to reclaim sweep task=%s (session still live)", t.ID)
		return
	}
	pruned, _, err := d.PruneTasks([]string{t.ID})
	switch {
	case err != nil:
		uxlog.Log("[resident] delete: prune failed task=%s: %v", t.ID, err)
	case len(pruned) == 0:
		uxlog.Log("[resident] delete: prune skipped task=%s (deferred to reclaim sweep)", t.ID)
	default:
		uxlog.Log("[resident] delete: pruned task row %s", t.ID)
	}
}

func residentWorkerStackFollowUp(d *db.DB, x *model.Task) {
	all, err := d.Tasks()
	if err != nil {
		uxlog.Log("[resident] delete: stacked-branch follow-up skipped task=%s: %v", x.ID, err)
		return
	}
	byBase := make(map[string]*model.Task, len(all))
	for _, t := range all {
		if t.BaseBranch != "" {
			byBase[t.BaseBranch] = t
		}
	}
	n := reconcileStackedBranches(context.Background(), d, d.Config(), byBase, x, map[string]mergesafety.Verdict{})
	uxlog.Log("[resident] delete: stacked-branch follow-up task=%s deleted=%d", x.ID, n)
}
