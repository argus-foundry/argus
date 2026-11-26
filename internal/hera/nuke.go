package hera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	heramodel "github.com/argus-foundry/argus/internal/hera/model"

	"github.com/argus-foundry/argus/internal/agent"
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

// archiveNukedTask archives a reclaimed task (in_review advances to complete).
func archiveNukedTask(d *db.DB, taskID string) {
	releaseSession, acquired, err := d.TryLockSessionMutation(taskID)
	if err != nil || !acquired {
		uxlog.Log("[hera] nuke: archive session lock unavailable task=%s err=%v", taskID, err)
		return
	}
	defer releaseSession()
	unlock, acquired, err := d.TryLockHeraReclaim(taskID)
	if err != nil || !acquired {
		uxlog.Log("[hera] nuke: archive deferred task=%s err=%v", taskID, err)
		return
	}
	defer unlock()
	archiveNukedTaskLocked(d, taskID)
}

func archiveNukedTaskLocked(d *db.DB, taskID string) bool {
	pending, err := d.HasUnsettledHeraNuke(taskID)
	if err != nil || !pending {
		return false
	}
	bindings, err := d.ListHeraLiveBindingsByTask(taskID)
	if err != nil || len(bindings) != 0 {
		uxlog.Log("[hera] nuke: archive binding guard task=%s live=%d err=%v", taskID, len(bindings), err)
		return false
	}
	t, err := d.Get(taskID)
	if err != nil || t == nil {
		uxlog.Log("[hera] nuke: task %s not found, skip archive: %v", taskID, err)
		return false
	}
	if err := d.SetArchived(t.ID, true); err != nil {
		uxlog.Log("[hera] nuke: archive task failed task=%s: %v", t.ID, err)
		return false
	}
	if t.Status == model.StatusInReview {
		if sErr := d.SetStatus(t.ID, model.StatusComplete); sErr != nil {
			uxlog.Log("[hera] nuke: complete task failed task=%s: %v", t.ID, sErr)
		}
	}
	return true
}

// CheckNukeTask refuses (db.ErrGraduationPending, wrapped) when taskID still
// has a graduation restart pending, and fails CLOSED when that state cannot be
// read. Callers use it to surface the refusal before any side effect.
func CheckNukeTask(d *db.DB, taskID string) error {
	if taskID == "" {
		return nil
	}
	pending, err := d.GraduationPendingErr(taskID)
	if err != nil {
		return fmt.Errorf("graduation pending check for task %s: %w", taskID, err)
	}
	if pending {
		return fmt.Errorf("%w (task %s)", db.ErrGraduationPending, taskID)
	}
	return nil
}

// CheckNukeSubtree scans every role of every nukeable orchestrator in subtree
// and returns the first graduation-pending refusal, so a cascade nuke refuses
// as a whole before any side effect.
func CheckNukeSubtree(d *db.DB, subtree []*heramodel.OrchView) error {
	for _, o := range subtree {
		if residentOrchestrator(d, o.ID) {
			continue
		}
		for i := range o.Roles {
			if err := CheckNukeTask(d, o.Roles[i].TaskID); err != nil {
				return err
			}
		}
	}
	return nil
}

// NukeSubtree nukes every orchestrator in subtree, archiving tasks bound only inside it and preserving the rest.
// Roles are nuked before their task is archived so the reclaim prune sees no live binding.
// A pending graduate anywhere in the subtree (or an unreadable check) refuses the
// WHOLE cascade with no side effect and a non-nil error.
func NukeSubtree(d *db.DB, runner NukeRunner, subtree []*heramodel.OrchView) (reclaimed []string, background func(), err error) {
	if err := CheckNukeSubtree(d, subtree); err != nil {
		uxlog.Log("[resident] cascade nuke refused: %v", err)
		return nil, func() {}, err
	}
	ids := subtreeIDSet(d, subtree)
	seen := make(map[string]bool)
	var operationErr error
	blockedTasks := make(map[string]bool)
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
				if err := d.NukeHeraRoleAndEndBindingAs(r.RoleID, db.HeraActorHuman); err != nil {
					uxlog.Log("[hera] nuke subtree: nuke role %d failed: %v", r.RoleID, err)
					operationErr = errors.Join(operationErr, fmt.Errorf("nuke role %d: %w", r.RoleID, err))
					blockedTasks[r.TaskID] = true
				}
			}
			if reclaim != "" {
				reclaimed = append(reclaimed, reclaim)
			}
		}
	}
	reclaimed = slices.DeleteFunc(reclaimed, func(taskID string) bool { return blockedTasks[taskID] })
	for _, id := range reclaimed {
		archiveNukedTask(d, id)
	}
	background = reclaimBackground(d, runner, reclaimed)
	if operationErr != nil {
		return reclaimed, background, operationErr
	}
	for _, o := range subtree {
		if !ids[o.ID] {
			continue
		}
		if err := d.NukeHeraOrchestrator(o.ID); err != nil {
			uxlog.Log("[hera] nuke subtree: nuke orchestrator %d failed: %v", o.ID, err)
			operationErr = errors.Join(operationErr, fmt.Errorf("nuke orchestrator %d: %w", o.ID, err))
		}
	}
	return reclaimed, background, operationErr
}

// NukeSingleRole nukes one role. Its task is reclaimed only when it is solely
// bound to this role; a multi-bound task is preserved untouched.
func NukeSingleRole(d *db.DB, runner NukeRunner, r *heramodel.RoleView) (background func(), err error) {
	if t, err := d.Get(r.TaskID); err == nil && t.IsResident() {
		return func() {}, db.ErrResidentProtected
	}

	if err := CheckNukeTask(d, r.TaskID); err != nil {
		uxlog.Log("[resident] nuke role %d refused: %v", r.RoleID, err)
		return func() {}, err
	}

	var reclaimed []string
	if r.Live && r.TaskID != "" {
		live, err := d.ListHeraLiveBindingsByTask(r.TaskID)
		if err == nil && len(live) == 1 && live[0].RoleID == r.RoleID {
			reclaimed = []string{r.TaskID}
		}
	}
	if err := d.NukeHeraRoleAndEndBindingAs(r.RoleID, db.HeraActorHuman); err != nil {
		uxlog.Log("[hera] nuke role %d failed: %v", r.RoleID, err)
		return func() {}, err
	}
	for _, id := range reclaimed {
		archiveNukedTask(d, id)
	}
	return reclaimBackground(d, runner, reclaimed), nil
}

func reclaimBackground(d *db.DB, runner NukeRunner, taskIDs []string) func() {
	if runner == nil || len(taskIDs) == 0 {
		return func() {}
	}
	return func() {
		for _, id := range taskIDs {
			stopNukedTask(d, runner, id)
		}
		if _, err := ReconcileHeraReclaims(d, runner); err != nil {
			uxlog.Log("[hera] nuke: reclaim sweep failed: %v", err)
		}
	}
}

func stopNukedTask(d *db.DB, runner NukeRunner, taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	unlock, err := d.LockSessionMutation(ctx, taskID)
	if err != nil {
		uxlog.Log("[hera] nuke: session lock failed task=%s err=%v", taskID, err)
		return
	}
	defer unlock()
	release, err := d.LockHeraReclaim(ctx, taskID)
	if err != nil {
		uxlog.Log("[hera] nuke: reclaim lock failed task=%s err=%v", taskID, err)
		return
	}
	defer release()
	if archiveNukedTaskLocked(d, taskID) {
		requestNukeStopLocked(d, runner, taskID)
	}
}

// RetryNukeStop replays a deferred nuke after owner prelaunch settles.
// The periodic daemon pass leaves contended operations to the next tick.
func RetryNukeStop(d *db.DB, runner NukeRunner, taskID string) {
	unlock, acquired, err := d.TryLockSessionMutation(taskID)
	if err != nil || !acquired {
		return
	}
	defer unlock()
	release, acquired, err := d.TryLockHeraReclaim(taskID)
	if err != nil || !acquired {
		return
	}
	defer release()
	if archiveNukedTaskLocked(d, taskID) {
		requestNukeStopLocked(d, runner, taskID)
	}
}

func requestNukeStopLocked(d *db.DB, runner NukeRunner, taskID string) {
	pending, err := d.HeraReclaimStopPending(taskID)
	if err != nil || pending {
		return
	}
	provider, providerOK := runner.(agent.SessionProvider)
	owner, ownerOK := runner.(agent.HeraReclaimSessionOwner)
	if providerOK && ownerOK {
		sess, err := agent.AttachableSession(provider, taskID)
		if err != nil {
			uxlog.Log("[hera] nuke: could not verify session task=%s err=%v", taskID, err)
		} else if sess != nil {
			if err := RequestScopedReclaimStop(d, owner, taskID, sess.PID()); err != nil {
				uxlog.Log("[hera] nuke: scoped stop failed task=%s err=%v", taskID, err)
			}
		}
		return
	}
	if runner.HasSession(taskID) {
		if err := runner.Stop(taskID); err != nil {
			uxlog.Log("[hera] nuke: stop session failed task=%s err=%v", taskID, err)
		}
	}
}

// RequestScopedReclaimStop records the active PTY before signaling it. Callers
// hold both task lifecycle locks and have checked binding eligibility.
func RequestScopedReclaimStop(d *db.DB, owner agent.HeraReclaimSessionOwner, taskID string, pid int) error {
	armed, err := d.ArmHeraReclaimStop(taskID, pid)
	if err != nil {
		return err
	}
	if !armed {
		return db.ErrHeraReclaimInProgress
	}
	if err := owner.StopTreeForPID(taskID, pid); err != nil {
		if !errors.Is(err, agent.ErrSessionStopUnconfirmed) {
			err = errors.Join(err, d.ClearHeraReclaimStop(taskID, pid))
		}
		return err
	}
	return nil
}

func residentOrchestrator(d *db.DB, id int64) bool {
	resident, err := d.IsResidentOrchestrator(id)
	// An unreadable orchestrator must never be treated as safe to destroy.
	return err != nil || resident
}
