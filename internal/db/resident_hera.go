package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const ResidentOrchestratorName = "resident"
const HeraMetaKeyResidentWorker = "resident_worker"

// EnsureResidentOrchestrator creates the standing team atomically. An existing
// name is accepted only when its coordinator is already this resident task.
func (d *DB) EnsureResidentOrchestrator(taskID string) (*HeraOrchestrator, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, err := d.residentTaskLocked()
	if err != nil {
		return nil, err
	}
	if task == nil || task.ID != taskID {
		return nil, ErrResidentProtected
	}
	if o, err := d.heraOrchestratorByActiveName(ResidentOrchestratorName); err == nil {
		var n int
		err = d.conn.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id WHERE b.orchestrator_id=? AND b.argus_task_id=? AND b.ended_at IS NULL AND r.kind='coordinator' AND r.name='resident'`, o.ID, taskID).Scan(&n)
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, fmt.Errorf("%w: orchestrator resident is already in use", ErrResidentProtected)
		}
		return o, nil
	} else if !errors.Is(err, ErrHeraNotFound) {
		return nil, err
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	now := formatTime(time.Now())
	res, err := tx.Exec(`INSERT INTO hera_orchestrators (name, created_at, base_branch) VALUES (?, ?, '')`, ResidentOrchestratorName, now)
	if err != nil {
		return nil, err
	}
	oid, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	r, err := insertHeraRole(tx, CreateHeraRoleInput{OrchestratorID: oid, Name: ResidentOrchestratorName, Kind: HeraKindCoordinator}, now)
	if err != nil {
		return nil, err
	}
	_, err = insertHeraBinding(tx, CreateHeraBindingInput{RoleID: r.ID, OrchestratorID: oid, ArgusTaskID: taskID, WorktreePath: task.Worktree}, now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	uxlog.Log("[resident] standing orchestrator created id=%d task=%s", oid, taskID)
	return d.heraOrchestratorByID(oid)
}

// residentHeraGuard protects the standing coordinator, while workers retain
// ordinary lifecycle operations. Called with d.mu held.
func (d *DB) residentHeraGuard(kind string, id int64) error {
	var n int
	var err error
	switch kind {
	case "orchestrator":
		err = d.conn.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id WHERE b.orchestrator_id=? AND b.ended_at IS NULL AND t.kind=?`, id, model.KindResident).Scan(&n)
	case "role":
		err = d.conn.QueryRow(`SELECT COUNT(*) FROM hera_roles r JOIN hera_bindings b ON b.role_id=r.id JOIN tasks t ON t.id=b.argus_task_id WHERE r.id=? AND t.kind=? AND b.ended_at IS NULL`, id, model.KindResident).Scan(&n)
	case "binding":
		err = d.conn.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id WHERE b.id=? AND t.kind=?`, id, model.KindResident).Scan(&n)
	default:
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrResidentProtected
	}
	return nil
}

// IsResidentWorker includes the durable pre-launch marker and live membership;
// either is sufficient. Lookup failures are returned for fail-closed callers.
func (d *DB) IsResidentWorker(taskID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	err := d.conn.QueryRow(`SELECT (EXISTS(SELECT 1 FROM task_meta WHERE task_id=? AND namespace='hera' AND key='resident_worker' AND value='true') OR EXISTS(SELECT 1 FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id JOIN hera_orchestrators o ON o.id=b.orchestrator_id WHERE b.argus_task_id=? AND b.ended_at IS NULL AND r.kind='worker' AND o.name='resident' AND EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=o.id AND rb.ended_at IS NULL AND rt.kind='resident')))`, taskID, taskID).Scan(&n)
	return n > 0, err
}

func (d *DB) ResidentWorkerCount() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id JOIN hera_orchestrators o ON o.id=b.orchestrator_id WHERE b.ended_at IS NULL AND r.kind='worker' AND o.name='resident' AND o.archived_at IS NULL AND EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=o.id AND rb.ended_at IS NULL AND rt.kind='resident')`).Scan(&n)
	return n, err
}

func (d *DB) ResidentMaxWorkersFromConfigToml() bool { return d.cfgLoader.ResidentMaxWorkersDefined() }

// IsResidentOrchestrator uses the resident task's binding as identity. A legacy
// user orchestrator named resident is a collision, not the standing team, and
// must remain visible and renameable so the operator can resolve it.
func (d *DB) IsResidentOrchestrator(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := d.residentHeraGuard("orchestrator", id)
	if errors.Is(err, ErrResidentProtected) {
		return true, nil
	}
	return false, err
}

// ListResidentOrchestrators returns the standing orchestrator as a one-element
// list (empty before the daemon has created it), the resident tab's analogue of
// ListHeraOrchestrators. Identity is the live binding to the resident task,
// never the name: a legacy user orchestrator called "resident" is not returned.
// Archived rows are returned only when includeArchived, matching the generic
// list's contract.
func (d *DB) ListResidentOrchestrators(includeArchived bool) ([]*HeraOrchestrator, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	query := `SELECT id, name, created_at, archived_at, pinned_at, nuked_at, base_branch, kanban_status FROM hera_orchestrators WHERE nuked_at IS NULL AND EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=hera_orchestrators.id AND rb.ended_at IS NULL AND rt.kind='resident')`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	rows, err := d.conn.Query(query + ` ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list resident orchestrators: %w", err)
	}
	defer rows.Close()
	var out []*HeraOrchestrator
	for rows.Next() {
		o, err := scanHeraOrchestrator(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
