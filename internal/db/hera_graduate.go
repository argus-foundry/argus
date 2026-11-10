package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const HeraEndReasonGraduated = "graduated"
const HeraMetaKeyGraduationPendingRestart = "graduation_pending_restart"

// GraduateHeraWorker transfers a standing worker into an independent team.
// The launch-policy change and durable restart intent commit with membership:
// no crash window can leave a graduate launching with unattended flags.
func (d *DB) GraduateHeraWorker(bindingID int64) (*MoveHeraBindingResult, error) {
	var out MoveHeraBindingResult
	err := d.WithTx(func(tx *sql.Tx) error {
		var roleID int64
		var name, project, prompt, taskID, worktree string
		err := tx.QueryRow(`SELECT r.id,r.name,COALESCE(NULLIF(r.argus_project,''),t.project),r.prompt,b.argus_task_id,b.worktree_path
			FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id JOIN hera_orchestrators o ON o.id=b.orchestrator_id JOIN tasks t ON t.id=b.argus_task_id
			WHERE b.id=? AND b.ended_at IS NULL AND r.kind='worker' AND r.nuked_at IS NULL AND r.archived_at IS NULL
			AND o.name=? AND o.archived_at IS NULL AND EXISTS
			(SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=o.id AND rb.ended_at IS NULL AND rt.kind='resident')`, bindingID, ResidentOrchestratorName).Scan(&roleID, &name, &project, &prompt, &taskID, &worktree)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrHeraNotFound
		}
		if err != nil {
			return err
		}
		if err = residentGuard(tx, taskID); err != nil {
			return err
		}
		var bindings int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM hera_bindings WHERE argus_task_id=? AND ended_at IS NULL`, taskID).Scan(&bindings); err != nil {
			return err
		}
		if bindings != 1 {
			return ErrHeraAmbiguous
		}
		base := name
		if base == ResidentOrchestratorName {
			base = "resident-graduate"
		}
		orchName, err := uniqueHeraOrchestratorName(tx, base)
		if err != nil {
			return err
		}
		now := formatTime(time.Now())
		if _, err = tx.Exec(`UPDATE hera_bindings SET ended_at=?,end_reason=? WHERE id=?`, now, HeraEndReasonGraduated, bindingID); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO hera_orchestrators(name,created_at,base_branch) VALUES (?,?,'')`, orchName, now)
		if err != nil {
			return err
		}
		oid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		r, err := insertHeraRole(tx, CreateHeraRoleInput{OrchestratorID: oid, Name: name, Kind: HeraKindCoordinator, ArgusProject: project, Prompt: prompt}, now)
		if err != nil {
			return err
		}
		b, err := insertHeraBinding(tx, CreateHeraBindingInput{RoleID: r.ID, OrchestratorID: oid, ArgusTaskID: taskID, WorktreePath: worktree}, now)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace='hera' AND key IN ('resident_worker','ready_to_close','pending_recycle')`, taskID); err != nil {
			return err
		}
		for _, kv := range [][2]string{{HeraMetaKeyGraduationPendingRestart, "true"}, {"graduated_from", ResidentOrchestratorName}, {HeraMetaKeyRole, string(HeraKindCoordinator)}} {
			if _, err = tx.Exec(`INSERT INTO task_meta(task_id,namespace,key,value,updated_at) VALUES (?,'hera',?,?,?) ON CONFLICT(task_id,namespace,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, taskID, kv[0], kv[1], now); err != nil {
				return err
			}
		}
		// Graduation promises atomic mail cleanup as well as atomic membership.
		if _, err = tx.Exec(`UPDATE hera_messages SET read_at=? WHERE to_role_id=? AND read_at IS NULL`, now, roleID); err != nil {
			return fmt.Errorf("graduate clear mail: %w", err)
		}
		out = MoveHeraBindingResult{OldOrchestratorName: ResidentOrchestratorName, OldRoleName: name, NewRole: r, NewBinding: b}
		return nil
	})
	if err != nil {
		return nil, err
	}
	uxlog.Log("[resident] graduated worker=%s task=%s role=%d", out.OldRoleName, out.NewBinding.ArgusTaskID, out.NewRole.ID)
	return &out, nil
}

// CompleteHeraGraduation commits the orientation exactly once with clearing
// the restart intent. A separate durable outbox key survives a crash before
// notifier registration; the task inbox retains the full orientation.
func (d *DB) CompleteHeraGraduation(taskID, body string) error {
	return d.WithTx(func(tx *sql.Tx) error {
		id := "graduation:" + taskID
		now := formatTime(time.Now())
		if _, err := tx.Exec(`INSERT INTO task_messages(id,from_task_id,to_task_id,kind,body,in_reply_to,created_at,read_at) VALUES (?,?,?,'note',?,'',?,'') ON CONFLICT(id) DO NOTHING`, id, model.SystemTaskID, taskID, body, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`, taskID, HeraMetaKeyGraduationPendingRestart); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO task_meta(task_id,namespace,key,value,updated_at) VALUES (?,'hera','graduation_orientation',?,?) ON CONFLICT(task_id,namespace,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, taskID, body, now)
		return err
	})
}
