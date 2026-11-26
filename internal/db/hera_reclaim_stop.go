package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/model"
)

var ErrHeraReclaimInProgress = errors.New("task reclaim is in progress")

// Older bindings lack an explicit outcome. A sibling that outlived the nuke
// or took over the task later supersedes that record. Compare whole seconds
// in UTC and the fractional part separately to preserve RFC3339Nano precision.
const legacyHeraNukeHasSuccessorSQL = `EXISTS (
	SELECT 1 FROM hera_bindings sibling
	WHERE sibling.argus_task_id=deleted.argus_task_id AND sibling.id<>deleted.id
	AND (sibling.ended_at IS NULL OR (
		unixepoch(substr(sibling.ended_at,1,19) || iif(substr(sibling.ended_at,-1)='Z','Z',substr(sibling.ended_at,-6))),
		iif(substr(sibling.ended_at,20,1)='.',CAST(substr(sibling.ended_at,20) AS REAL),0)
	) > (
		unixepoch(substr(deleted.ended_at,1,19) || iif(substr(deleted.ended_at,-1)='Z','Z',substr(deleted.ended_at,-6))),
		iif(substr(deleted.ended_at,20,1)='.',CAST(substr(deleted.ended_at,20) AS REAL),0)
	)))`

const heraNukeBindingSQL = `SELECT argus_task_id FROM hera_bindings deleted
	WHERE (nuke_reclaim='claimed' OR (nuke_reclaim='' AND end_reason='` + HeraEndReasonUserDeleted + `'
	AND id > COALESCE((SELECT hera_reclaim_settled_binding FROM tasks WHERE id=deleted.argus_task_id),0)
	AND NOT ` + legacyHeraNukeHasSuccessorSQL + `))`

const heraReclaimOwnsTaskSQL = `(hera_reclaim_pid<>0 OR (id IN (` + heraNukeBindingSQL + `
	AND (tasks.archived=1 OR deleted.nuke_reclaim='claimed'))
	AND id NOT IN (SELECT argus_task_id FROM hera_bindings WHERE ended_at IS NULL)))`

func heraReclaimAdmissionGuard(q rowQuerier, taskID string) error {
	var blocked bool
	if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM tasks WHERE id=? AND `+heraReclaimOwnsTaskSQL+`)`, taskID).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrHeraReclaimInProgress
	}
	return nil
}

// lockHeraTaskAdmission coordinates restoration and role binding with reclaim.
func (d *DB) lockHeraTaskAdmission(taskID string) (func(), error) {
	unlock, acquired, err := d.TryLockHeraReclaim(taskID)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, ErrHeraReclaimInProgress
	}
	d.mu.Lock()
	var blocked bool
	err = d.conn.QueryRow(`SELECT EXISTS (SELECT 1 FROM tasks WHERE id=? AND `+heraReclaimOwnsTaskSQL+`)
		OR (NOT EXISTS (SELECT 1 FROM tasks WHERE id=?) AND ? IN (`+heraNukeBindingSQL+`))`,
		taskID, taskID, taskID).Scan(&blocked)
	d.mu.Unlock()
	if err != nil || blocked {
		unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrHeraReclaimInProgress
	}
	return unlock, nil
}

// TryLockTaskOutsideReclaim protects a generic mutation's side effects from
// taking place on a task that already belongs to Hera reclaim.
func (d *DB) TryLockTaskOutsideReclaim(taskID string) (func(), error) {
	return d.lockHeraTaskAdmission(taskID)
}

// MarkHeraReclaimCleaned releases ownership when cleanup keeps an archived
// task. Each explicit claim settles on its binding, so clearing an older
// hidden role can still issue a fresh claim later.
func (d *DB) MarkHeraReclaimCleaned(taskID string) error {
	return d.WithTx(func(tx *sql.Tx) error {
		result, err := tx.Exec(`UPDATE tasks SET hera_reclaim_settled_binding=MAX(hera_reclaim_settled_binding, COALESCE((
			SELECT MAX(id) FROM hera_bindings WHERE argus_task_id=tasks.id AND nuke_reclaim='' AND end_reason=?),0))
			WHERE id=? AND archived=1 AND hera_reclaim_pid=0
			AND NOT EXISTS (SELECT 1 FROM hera_bindings WHERE argus_task_id=tasks.id AND ended_at IS NULL)`,
			HeraEndReasonUserDeleted, taskID)
		if err != nil {
			return err
		}
		matched, err := result.RowsAffected()
		if err != nil || matched == 0 {
			return err
		}
		_, err = tx.Exec(`UPDATE hera_bindings SET nuke_reclaim='settled' WHERE argus_task_id=? AND nuke_reclaim='claimed'`, taskID)
		return err
	})
}

// HasUnsettledHeraNuke revalidates queued reclaim after acquiring task locks.
func (d *DB) HasUnsettledHeraNuke(taskID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var unsettled bool
	err := d.conn.QueryRow(`SELECT ? IN (`+heraNukeBindingSQL+`)`, taskID).Scan(&unsettled)
	return unsettled, err
}

// ArmHeraReclaimStop records the outgoing PTY of an archived, active task.
// The caller holds the task's session mutation lock through the stop request.
func (d *DB) ArmHeraReclaimStop(taskID string, pid int) (bool, error) {
	if pid <= 0 {
		return false, errors.New("reclaim requires a session PID")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result, err := d.conn.Exec(`UPDATE tasks SET hera_reclaim_pid=?
		WHERE id=? AND archived=1 AND kind<>? AND hera_reclaim_pid=0`, pid, taskID, model.KindResident)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// ClearHeraReclaimStop disarms the intent after a failed stop. It only clears
// an intent for the same process.
func (d *DB) ClearHeraReclaimStop(taskID string, pid int) error {
	_, err := d.ConsumeHeraReclaimStop(taskID, pid)
	return err
}

// HeraReclaimStopPending reports whether cleanup still owes an exit relay
// for this task. Check it while holding the task's session mutation lock.
func (d *DB) HeraReclaimStopPending(taskID string) (bool, error) {
	pid, err := d.HeraReclaimStopPID(taskID)
	return pid != 0, err
}

// HeraReclaimStopPID returns the armed process identity, or zero if disarmed.
func (d *DB) HeraReclaimStopPID(taskID string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var pid int
	err := d.conn.QueryRow(`SELECT hera_reclaim_pid FROM tasks WHERE id=?`, taskID).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	return pid, err
}

// PendingHeraReclaimStops lists tasks whose stop still needs an exit relay.
// A caller must re-read the PID under the session lock before using it.
func (d *DB) PendingHeraReclaimStops() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id FROM tasks WHERE hera_reclaim_pid<>0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ConsumeHeraReclaimStop consumes the intent for the observed owner exactly
// once. It reports whether the task is still eligible for reclaim completion.
// The session owner handles the status transition and live worker policy.
func (d *DB) ConsumeHeraReclaimStop(taskID string, pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var status, kind string
	var archived int
	err := d.conn.QueryRow(`UPDATE tasks SET hera_reclaim_pid=0
		WHERE id=? AND hera_reclaim_pid=? RETURNING status, archived, kind`, taskID, pid).Scan(&status, &archived, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return archived == 1 && status == model.StatusInProgress.String() && kind != model.KindResident, err
}
