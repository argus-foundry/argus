package db

import (
	"database/sql"
	"errors"

	"github.com/argus-foundry/argus/internal/model"
)

// ArmHeraReclaimStop records the outgoing PTY of an archived, active task.
// The caller holds the task's session mutation lock through the stop request.
func (d *DB) ArmHeraReclaimStop(taskID string, pid int) (bool, error) {
	if pid <= 0 {
		return false, errors.New("reclaim requires a session PID")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result, err := d.conn.Exec(`UPDATE tasks SET hera_reclaim_pid=?
		WHERE id=? AND archived=1 AND status=? AND kind<>?`, pid, taskID, model.StatusInProgress.String(), model.KindResident)
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
	d.mu.Lock()
	defer d.mu.Unlock()
	var pid int
	err := d.conn.QueryRow(`SELECT hera_reclaim_pid FROM tasks WHERE id=?`, taskID).Scan(&pid)
	return pid != 0, err
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
