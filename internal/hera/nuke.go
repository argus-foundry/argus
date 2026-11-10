package hera

import (
	heramodel "github.com/argus-foundry/argus/internal/hera/model"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// NukeRunner is the narrow session surface the daemon-side nuke needs.
type NukeRunner interface {
	SessionChecker
	Stop(taskID string) error
}

// NukePreview is what a subtree nuke would act on, for confirm dialogs.
type NukePreview struct {
	Orchestrators int `json:"orchestrators"`
	Agents        int `json:"agents"`
	Worktrees     int `json:"worktrees"`
	Preserved     int `json:"preserved"`
}

// TaskBoundOutside reports whether taskID holds a live binding to an
// orchestrator outside subtreeIDs; a query error errs on the side of preserving.
func TaskBoundOutside(d *db.DB, taskID string, subtreeIDs map[int64]bool) bool {
	live, err := d.ListHeraLiveBindingsByTask(taskID)
	if err != nil {
		return true
	}
	for _, b := range live {
		if !subtreeIDs[b.OrchestratorID] {
			return true
		}
	}
	return false
}

func subtreeIDSet(d *db.DB, subtree []*heramodel.OrchView) map[int64]bool {
	ids := make(map[int64]bool, len(subtree))
	for _, o := range subtree {
		if residentOrchestrator(d, o.ID) {
			continue
		}
		ids[o.ID] = true
	}
	return ids
}

// PreviewNukeSubtree counts what NukeSubtree would do without mutating anything.
func PreviewNukeSubtree(d *db.DB, subtree []*heramodel.OrchView) NukePreview {
	p := NukePreview{}
	ids := subtreeIDSet(d, subtree)
	seen := make(map[string]bool)
	for _, o := range subtree {
		if residentOrchestrator(d, o.ID) {
			continue
		}
		p.Orchestrators++
		for i := range o.Roles {
			r := &o.Roles[i]
			if !r.Live || r.TaskID == "" {
				continue
			}
			if r.Kind != db.HeraKindCoordinator {
				p.Agents++
			}
			if seen[r.TaskID] {
				continue
			}
			seen[r.TaskID] = true
			if TaskBoundOutside(d, r.TaskID, ids) {
				p.Preserved++
			} else {
				p.Worktrees++
			}
		}
	}
	return p
}

// nukeRoleRow ends the role's live binding (user_deleted) and stamps it NUKED —
// never a hard delete.
func nukeRoleRow(d *db.DB, roleID int64, actor db.HeraActor) error {
	if b, err := d.HeraLiveBindingByRole(roleID); err == nil && b != nil {
		if eErr := d.EndHeraBinding(b.ID, db.HeraEndReasonUserDeleted); eErr != nil {
			uxlog.Log("[hera] nuke role %d: end binding %d failed: %v", roleID, b.ID, eErr)
		}
	}
	return d.NukeHeraRoleAs(roleID, actor)
}

// archiveNukedTask archives a reclaimed task (in_review advances to complete).
func archiveNukedTask(d *db.DB, taskID string) {
	t, err := d.Get(taskID)
	if err != nil || t == nil {
		uxlog.Log("[hera] nuke: task %s not found, skip archive: %v", taskID, err)
		return
	}
	if t.Status == model.StatusInReview {
		if sErr := d.SetStatus(t.ID, model.StatusComplete); sErr != nil {
			uxlog.Log("[hera] nuke: complete task failed task=%s: %v", t.ID, sErr)
		}
	}
	if aErr := d.SetArchived(t.ID, true); aErr != nil {
		uxlog.Log("[hera] nuke: archive task failed task=%s: %v", t.ID, aErr)
	}
}

// NukeSubtree nukes every orchestrator in subtree, archiving tasks bound only inside it and preserving the rest.
// Roles are nuked before their task is archived so the reclaim prune sees no live binding.
func NukeSubtree(d *db.DB, runner NukeRunner, subtree []*heramodel.OrchView) (reclaimed []string, background func()) {
	ids := subtreeIDSet(d, subtree)
	seen := make(map[string]bool)
	for _, o := range subtree {
		if residentOrchestrator(d, o.ID) {
			continue
		}
		for i := range o.Roles {
			r := &o.Roles[i]
			reclaim := ""
			if r.Live && r.TaskID != "" && !seen[r.TaskID] && !TaskBoundOutside(d, r.TaskID, ids) {
				seen[r.TaskID] = true
				reclaim = r.TaskID
			}
			if r.Live {
				if err := nukeRoleRow(d, r.RoleID, db.HeraActorHuman); err != nil {
					uxlog.Log("[hera] nuke subtree: nuke role %d failed: %v", r.RoleID, err)
				}
			}
			if reclaim != "" {
				archiveNukedTask(d, reclaim)
				reclaimed = append(reclaimed, reclaim)
			}
		}
		if err := d.NukeHeraOrchestrator(o.ID); err != nil {
			uxlog.Log("[hera] nuke subtree: nuke orchestrator %d failed: %v", o.ID, err)
		}
	}
	return reclaimed, reclaimBackground(d, runner, reclaimed)
}

// NukeSingleRole nukes one role. Its task is reclaimed only when it is solely
// bound to this role; a multi-bound task is preserved untouched.
func NukeSingleRole(d *db.DB, runner NukeRunner, r *heramodel.RoleView) (background func()) {
	return NukeSingleRoleAs(d, runner, r, db.HeraActorHuman)
}

// NukeSingleRoleAs is NukeSingleRole with the acting party named: the
// resident's own hera_delete_worker passes db.HeraActorResident so the
// resident is not told about its own delete (6.4), everything else is human.
func NukeSingleRoleAs(d *db.DB, runner NukeRunner, r *heramodel.RoleView, actor db.HeraActor) (background func()) {
	if t, err := d.Get(r.TaskID); err == nil && t.IsResident() {
		return func() {}
	}

	var reclaimed []string
	if r.Live && r.TaskID != "" {
		live, err := d.ListHeraLiveBindingsByTask(r.TaskID)
		if err == nil && len(live) == 1 && live[0].RoleID == r.RoleID {
			reclaimed = []string{r.TaskID}
		}
	}
	if err := nukeRoleRow(d, r.RoleID, actor); err != nil {
		uxlog.Log("[hera] nuke role %d failed: %v", r.RoleID, err)
	}
	for _, id := range reclaimed {
		archiveNukedTask(d, id)
	}
	return reclaimBackground(d, runner, reclaimed)
}

func reclaimBackground(d *db.DB, runner NukeRunner, taskIDs []string) func() {
	if runner == nil || len(taskIDs) == 0 {
		return func() {}
	}
	return func() {
		for _, id := range taskIDs {
			if runner.HasSession(id) {
				if err := runner.Stop(id); err != nil {
					uxlog.Log("[hera] nuke: stop session failed task=%s: %v", id, err)
				}
			}
		}
		if _, err := ReconcileHeraReclaims(d, runner); err != nil {
			uxlog.Log("[hera] nuke: reclaim sweep failed: %v", err)
		}
	}
}

func residentOrchestrator(d *db.DB, id int64) bool {
	resident, err := d.IsResidentOrchestrator(id)
	// An unreadable orchestrator must never be treated as safe to destroy.
	return err != nil || resident
}
