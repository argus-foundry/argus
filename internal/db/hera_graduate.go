package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const HeraEndReasonGraduated = "graduated"
const HeraMetaKeyGraduationPendingRestart = "graduation_pending_restart"

// HeraMetaKeyGraduationBindingID records the new coordinator binding id so the
// restart watcher resolves the graduate by id, not by a (possibly ambiguous)
// by-task lookup.
const HeraMetaKeyGraduationBindingID = "graduation_binding_id"

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
		for _, kv := range [][2]string{{HeraMetaKeyGraduationPendingRestart, "true"}, {HeraMetaKeyGraduationBindingID, strconv.FormatInt(b.ID, 10)}, {"graduated_from", ResidentOrchestratorName}, {HeraMetaKeyRole, string(HeraKindCoordinator)}} {
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
// the restart intent, without touching the task row. Thin form of
// CompleteHeraGraduationAt (any observed phase, no PID/status write).
func (d *DB) CompleteHeraGraduation(taskID, body string) error {
	_, err := d.CompleteHeraGraduationAt(taskID, "", body, -1)
	return err
}

// CompleteHeraGraduationAt is the daemon watcher's compare-and-set completion.
// In ONE transaction it deletes the pending-restart marker only WHERE its value
// still equals phase ("" = any value), and only when that removed a row does it
// write the PID/in_progress status (targeted UPDATE, never a stale full-row
// Update that would un-archive the task) and insert the orientation message and
// outbox key. A human abandon (+archive/delete) between the watcher's snapshot
// and this call removes the marker, so it returns (false, nil) and changes
// nothing. pid < 0 skips the task-row write. A separate durable outbox key
// survives a crash before notifier registration; the task inbox retains the
// full orientation.
func (d *DB) CompleteHeraGraduationAt(taskID, phase, body string, pid int) (bool, error) {
	var completed bool
	var statusFrom, statusTo model.Status
	err := d.WithTx(func(tx *sql.Tx) error {
		q := `DELETE FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`
		args := []any{taskID, HeraMetaKeyGraduationPendingRestart}
		if phase != "" {
			q += ` AND value=?`
			args = append(args, phase)
		}
		res, err := tx.Exec(q, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		if pid >= 0 {
			var stStr, startedStr, endedStr string
			if err := tx.QueryRow(`SELECT status, started_at, ended_at FROM tasks WHERE id=?`, taskID).Scan(&stStr, &startedStr, &endedStr); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// Task row gone: nothing to complete. Returning nil COMMITS the
					// marker delete above on purpose, so the watcher stops looping on
					// a dead task; completed stays false and no orientation is written.
					return nil
				}
				return err
			}
			statusFrom, _ = model.ParseStatus(stStr)
			tmp := &model.Task{Status: statusFrom, StartedAt: parseTime(startedStr), EndedAt: parseTime(endedStr)}
			tmp.SetStatus(model.StatusInProgress)
			statusTo = tmp.Status
			if _, err := tx.Exec(`UPDATE tasks SET agent_pid=?, status=?, started_at=?, ended_at=? WHERE id=?`, pid, tmp.Status.String(), formatTime(tmp.StartedAt), formatTime(tmp.EndedAt), taskID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`, taskID, HeraMetaKeyGraduationBindingID); err != nil {
			return err
		}
		id := "graduation:" + taskID
		now := formatTime(time.Now())
		if _, err := tx.Exec(`INSERT INTO task_messages(id,from_task_id,to_task_id,kind,body,in_reply_to,created_at,read_at) VALUES (?,?,?,'note',?,'',?,'') ON CONFLICT(id) DO NOTHING`, id, model.SystemTaskID, taskID, body, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_meta(task_id,namespace,key,value,updated_at) VALUES (?,'hera','graduation_orientation',?,?) ON CONFLICT(task_id,namespace,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, taskID, body, now); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil || !completed {
		if err == nil {
			uxlog.Log("[resident] graduation completion skipped task=%s: pending marker changed or cleared (or task gone)", taskID)
		}
		return false, err
	}
	if statusFrom != statusTo {
		events.Emit(model.EventTypeTaskStatusChanged, taskID, map[string]string{"from": statusFrom.String(), "to": statusTo.String()})
	}
	return true, nil
}
