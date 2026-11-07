package db

import (
	"log/slog"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
)

// emitPlanChanged announces committed plan-graph changes for the given
// orchestrators. It must run after d.mu is released: the events sink re-enters
// the DB to persist the event.
func emitPlanChanged(orchIDs ...int64) {
	seen := make(map[int64]struct{}, len(orchIDs))
	for _, id := range orchIDs {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		events.Emit(model.EventTypeHeraPlanChanged, "", map[string]int64{"orchestrator_id": id})
	}
}

func (d *DB) orchOfRole(roleID int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	var id int64
	if err := d.conn.QueryRow(`SELECT orchestrator_id FROM hera_roles WHERE id=?`, roleID).Scan(&id); err != nil {
		return 0
	}
	return id
}

func (d *DB) orchsOfTask(taskID string) []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT DISTINCT orchestrator_id FROM hera_bindings WHERE argus_task_id=?`, taskID)
	if err != nil {
		slog.Warn("hera plan: orchestrators of task lookup failed", "err", err)
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (d *DB) emitPlanChangedForRole(roleID int64) { emitPlanChanged(d.orchOfRole(roleID)) }

func (d *DB) emitPlanChangedForTask(taskID string) { emitPlanChanged(d.orchsOfTask(taskID)...) }

func (d *DB) CreateHeraPlannedRole(in CreateHeraRoleInput) (*HeraRole, error) {
	r, err := d.createHeraPlannedRole(in)
	if err == nil {
		emitPlanChanged(r.OrchestratorID)
	}
	return r, err
}

func (d *DB) AddHeraBlock(blockedRoleID, blockerRoleID int64) error {
	if err := d.addHeraBlock(blockedRoleID, blockerRoleID); err != nil {
		return err
	}
	d.emitPlanChangedForRole(blockedRoleID)
	return nil
}

func (d *DB) CreateHeraPlan(orchID int64, nodes []HeraPlannedNodeSpec, edges []HeraBlockSpec) ([]*HeraRole, error) {
	created, err := d.createHeraPlan(orchID, nodes, edges)
	if err == nil {
		emitPlanChanged(orchID)
	}
	return created, err
}

func (d *DB) RemoveHeraBlock(blockedRoleID, blockerRoleID int64) error {
	if err := d.removeHeraBlock(blockedRoleID, blockerRoleID); err != nil {
		return err
	}
	d.emitPlanChangedForRole(blockedRoleID)
	return nil
}

func (d *DB) UpdateHeraPlannedNode(roleID int64, prompt, project string) error {
	if err := d.updateHeraPlannedNode(roleID, prompt, project); err != nil {
		return err
	}
	d.emitPlanChangedForRole(roleID)
	return nil
}

func (d *DB) CancelHeraPlannedNode(roleID int64) error {
	if err := d.cancelHeraPlannedNode(roleID); err != nil {
		return err
	}
	d.emitPlanChangedForRole(roleID)
	return nil
}

func (d *DB) CreateHeraRole(in CreateHeraRoleInput) (*HeraRole, error) {
	r, err := d.createHeraRole(in)
	if err == nil {
		emitPlanChanged(r.OrchestratorID)
	}
	return r, err
}

func (d *DB) ArchiveHeraRole(id int64) error {
	if err := d.archiveHeraRole(id); err != nil {
		return err
	}
	d.emitPlanChangedForRole(id)
	return nil
}

func (d *DB) UnarchiveHeraRole(id int64) error {
	if err := d.unarchiveHeraRole(id); err != nil {
		return err
	}
	d.emitPlanChangedForRole(id)
	return nil
}

func (d *DB) NukeHeraRole(id int64) error {
	if err := d.nukeHeraRole(id); err != nil {
		return err
	}
	d.emitPlanChangedForRole(id)
	return nil
}

func (d *DB) RenameHeraRole(id int64, newName string) error {
	if err := d.renameHeraRole(id, newName); err != nil {
		return err
	}
	d.emitPlanChangedForRole(id)
	return nil
}

func (d *DB) DeleteHeraRole(id int64) error {
	orch := d.orchOfRole(id)
	if err := d.deleteHeraRole(id); err != nil {
		return err
	}
	emitPlanChanged(orch)
	return nil
}

func (d *DB) CreateHeraBinding(in CreateHeraBindingInput) (*HeraBinding, error) {
	b, err := d.createHeraBinding(in)
	if err == nil {
		emitPlanChanged(b.OrchestratorID)
	}
	return b, err
}

func (d *DB) CreateHeraRoleWithBinding(roleIn CreateHeraRoleInput, taskID, worktreePath string) (*HeraRole, *HeraBinding, error) {
	r, b, err := d.createHeraRoleWithBinding(roleIn, taskID, worktreePath)
	if err == nil {
		emitPlanChanged(r.OrchestratorID)
	}
	return r, b, err
}

func (d *DB) MoveHeraBinding(oldBindingID int64, roleIn CreateHeraRoleInput, taskID, worktreePath string) (*MoveHeraBindingResult, error) {
	var oldOrch int64
	d.mu.Lock()
	_ = d.conn.QueryRow(`SELECT orchestrator_id FROM hera_bindings WHERE id=?`, oldBindingID).Scan(&oldOrch)
	d.mu.Unlock()
	res, err := d.moveHeraBinding(oldBindingID, roleIn, taskID, worktreePath)
	if err == nil {
		emitPlanChanged(oldOrch, res.NewRole.OrchestratorID)
	}
	return res, err
}

func (d *DB) EndHeraBinding(bindingID int64, reason string) error {
	if err := d.endHeraBinding(bindingID, reason); err != nil {
		return err
	}
	d.mu.Lock()
	var orch int64
	_ = d.conn.QueryRow(`SELECT orchestrator_id FROM hera_bindings WHERE id=?`, bindingID).Scan(&orch)
	d.mu.Unlock()
	emitPlanChanged(orch)
	return nil
}

func (d *DB) EndHeraBindingsForTask(taskID, reason string) (int, error) {
	n, err := d.endHeraBindingsForTask(taskID, reason)
	if err == nil && n > 0 {
		d.emitPlanChangedForTask(taskID)
	}
	return n, err
}

func (d *DB) UpsertHeraRoleStatus(roleID int64, status HeraRoleStatusValue) error {
	if err := d.upsertHeraRoleStatus(roleID, status); err != nil {
		return err
	}
	d.emitPlanChangedForRole(roleID)
	return nil
}

func (d *DB) ArchiveHeraOrchestrator(id int64) error {
	if err := d.archiveHeraOrchestrator(id); err != nil {
		return err
	}
	emitPlanChanged(id)
	return nil
}

func (d *DB) NukeHeraOrchestrator(id int64) error {
	if err := d.nukeHeraOrchestrator(id); err != nil {
		return err
	}
	emitPlanChanged(id)
	return nil
}

func (d *DB) CreateHeraOrchLink(parentOrchID, childOrchID, parentRoleID int64) error {
	if err := d.createHeraOrchLink(parentOrchID, childOrchID, parentRoleID); err != nil {
		return err
	}
	emitPlanChanged(parentOrchID, childOrchID)
	return nil
}

func (d *DB) DeleteHeraOrchLink(childOrchID int64) error {
	parent, _, _ := d.HeraOrchLink(childOrchID)
	if err := d.deleteHeraOrchLink(childOrchID); err != nil {
		return err
	}
	emitPlanChanged(parent, childOrchID)
	return nil
}
