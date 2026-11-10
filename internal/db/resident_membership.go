package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// standingTeamWhere matches an orchestrator row aliased `so` that is the
// resident's standing team: an active orchestrator named resident, OR any
// orchestrator holding a live binding of the resident task. Binds two args:
// ResidentOrchestratorName, model.KindResident.
//
// Membership of this team is what makes a task an unattended resident worker
// (IsResidentWorker), so only the resident's own ensure and resident spawn may
// add bindings to it, and only graduation may take a worker out.
const standingTeamWhere = `((so.name=? AND so.archived_at IS NULL) OR EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=so.id AND rb.ended_at IS NULL AND rt.kind=?))`

// isStandingTeam reports whether orchID is the resident's standing team.
func isStandingTeam(q rowQuerier, orchID int64) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM hera_orchestrators so WHERE so.id=? AND `+standingTeamWhere,
		orchID, ResidentOrchestratorName, model.KindResident).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("classify standing team: %w", err)
	}
	return n > 0, nil
}

// IsStandingTeamOrchestrator reports whether id is the resident's standing
// team under the same predicate that refuses ordinary binding inserts. Wider
// than IsResidentOrchestrator: an active legacy orchestrator named resident
// also counts, so nothing can join it before the operator renames it.
func (d *DB) IsStandingTeamOrchestrator(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return isStandingTeam(d.conn, id)
}

// insertHeraBindingGuarded is the ordinary binding insert. The standing-team
// check runs inside the INSERT itself, so it is atomic with the write; both
// the binding's orchestrator and the role's own orchestrator are checked.
func insertHeraBindingGuarded(ex execer, in CreateHeraBindingInput, now string) (*HeraBinding, error) {
	res, err := ex.Exec(
		`INSERT INTO hera_bindings (role_id, orchestrator_id, argus_task_id, worktree_path, started_at)
		 SELECT ?, ?, ?, ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM hera_orchestrators so
		   WHERE so.id IN (?, (SELECT orchestrator_id FROM hera_roles WHERE id=?)) AND `+standingTeamWhere+`)`,
		in.RoleID, in.OrchestratorID, in.ArgusTaskID, in.WorktreePath, now,
		in.OrchestratorID, in.RoleID, ResidentOrchestratorName, model.KindResident)
	if err != nil {
		return nil, fmt.Errorf("insert hera binding: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("insert hera binding: rows affected: %w", err)
	} else if n == 0 {
		uxlog.Log("[resident] binding refused: task=%s orchestrator=%d role=%d is the standing team", in.ArgusTaskID, in.OrchestratorID, in.RoleID)
		slog.Warn("[resident] binding refused: standing team", "task", in.ArgusTaskID, "orchestrator", in.OrchestratorID, "role", in.RoleID)
		return nil, fmt.Errorf("%w: only resident spawn may add members to the resident's standing team", ErrResidentProtected)
	}
	return heraBindingInserted(res, in, now)
}

// insertHeraBindingPrivileged skips the standing-team check. Only the
// resident's own ensure and CreateResidentWorkerRoleWithBinding may call it.
func insertHeraBindingPrivileged(ex execer, in CreateHeraBindingInput, now string) (*HeraBinding, error) {
	res, err := ex.Exec(
		`INSERT INTO hera_bindings (role_id, orchestrator_id, argus_task_id, worktree_path, started_at)
		 VALUES (?, ?, ?, ?, ?)`,
		in.RoleID, in.OrchestratorID, in.ArgusTaskID, in.WorktreePath, now)
	if err != nil {
		return nil, fmt.Errorf("insert hera binding: %w", err)
	}
	return heraBindingInserted(res, in, now)
}

func heraBindingInserted(res sql.Result, in CreateHeraBindingInput, now string) (*HeraBinding, error) {
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("insert hera binding: last insert id: %w", err)
	}
	return &HeraBinding{
		ID:             id,
		RoleID:         in.RoleID,
		OrchestratorID: in.OrchestratorID,
		ArgusTaskID:    in.ArgusTaskID,
		WorktreePath:   in.WorktreePath,
		StartedAt:      parseTime(now),
	}, nil
}

// CreateResidentWorkerRoleWithBinding is resident spawn's privileged
// counterpart to CreateHeraRoleWithBinding: the only way to add a worker to
// the standing team. The target must be the resident-bound orchestrator (not a
// legacy name collision), the role must be a worker, and the task must not be
// the resident itself.
func (d *DB) CreateResidentWorkerRoleWithBinding(roleIn CreateHeraRoleInput, taskID, worktreePath string) (*HeraRole, *HeraBinding, error) {
	if roleIn.Kind != HeraKindWorker {
		return nil, nil, fmt.Errorf("%w: resident spawn creates workers only", ErrResidentProtected)
	}
	var role *HeraRole
	var binding *HeraBinding
	err := d.WithTx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id WHERE b.orchestrator_id=? AND b.ended_at IS NULL AND t.kind=?`,
			roleIn.OrchestratorID, model.KindResident).Scan(&n); err != nil {
			return fmt.Errorf("resident worker binding: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: orchestrator %d is not the resident's standing team", ErrResidentProtected, roleIn.OrchestratorID)
		}
		if err := residentGuard(tx, taskID); err != nil {
			return err
		}
		now := formatTime(time.Now())
		r, err := insertHeraRole(tx, roleIn, now)
		if err != nil {
			return err
		}
		b, err := insertHeraBindingPrivileged(tx, CreateHeraBindingInput{
			RoleID:         r.ID,
			OrchestratorID: r.OrchestratorID,
			ArgusTaskID:    taskID,
			WorktreePath:   worktreePath,
		}, now)
		if err != nil {
			return err
		}
		role, binding = r, b
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return role, binding, nil
}

// refuseStandingTeamExit is MoveHeraBinding's guard: only graduation moves a
// worker out of the standing team. Called inside the move transaction.
func refuseStandingTeamExit(q rowQuerier, oldOrchID int64, taskID string) error {
	yes, err := isStandingTeam(q, oldOrchID)
	if err != nil {
		return err
	}
	if yes {
		uxlog.Log("[resident] move refused: task=%s leaving standing team orchestrator=%d", taskID, oldOrchID)
		slog.Warn("[resident] move refused: leaving standing team", "task", taskID, "orchestrator", oldOrchID)
		return fmt.Errorf("%w: only graduation moves a worker out of the resident's standing team", ErrResidentProtected)
	}
	return nil
}
