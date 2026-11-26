package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/model"
)

var ErrHeraReclaimInProgress = errors.New("task reclaim is in progress")

const heraReclaimSettledKey = "reclaim_settled_binding"

const heraNukeBindingSQL = `SELECT argus_task_id FROM hera_bindings deleted
	WHERE end_reason='` + HeraEndReasonUserDeleted + `' AND id > COALESCE((
		SELECT CAST(value AS INTEGER) FROM task_meta
		WHERE task_id=deleted.argus_task_id AND namespace='hera' AND key='` + heraReclaimSettledKey + `'),0)`

const heraReclaimOwnsTaskSQL = `(hera_reclaim_pid<>0 OR (archived=1 AND id IN (` + heraNukeBindingSQL + `)
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

// MarkHeraReclaimCleaned releases ownership when cleanup deliberately keeps an
// archived row. An ensuing nuke of a newer binding can reclaim it again.
func (d *DB) MarkHeraReclaimCleaned(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`INSERT INTO task_meta (task_id,namespace,key,value,updated_at)
		SELECT ?, 'hera', ?, CAST(MAX(id) AS TEXT), ? FROM hera_bindings
		WHERE argus_task_id=? AND end_reason=?
		HAVING COUNT(*) > 0
		ON CONFLICT(task_id,namespace,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		taskID, heraReclaimSettledKey, formatTime(time.Now()), taskID, HeraEndReasonUserDeleted)
	return err
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
