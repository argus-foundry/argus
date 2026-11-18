package db

import (
	"errors"
	"fmt"
)

// SetSessionIDIfEmpty records a backend-minted session ID only while the task
// has none, in one atomic statement so it can never overwrite an ID another
// path (post-exit capture, resume refresh) already stored. Reports whether this
// call wrote it.
func (d *DB) SetSessionIDIfEmpty(id, sessionID string) (bool, error) {
	if id == "" || sessionID == "" {
		return false, errors.New("SetSessionIDIfEmpty: task id and session id are required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE tasks SET session_id=? WHERE id=? AND session_id=''`, sessionID, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, d.requireTaskLocked(id, n)
}

// ReplaceSessionID overwrites a task's session ID (a Codex /new moved the
// conversation to a new thread). Reports whether the stored value changed.
func (d *DB) ReplaceSessionID(id, sessionID string) (bool, error) {
	if id == "" || sessionID == "" {
		return false, errors.New("ReplaceSessionID: task id and session id are required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE tasks SET session_id=? WHERE id=? AND session_id<>?`, sessionID, id, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, d.requireTaskLocked(id, n)
}

// requireTaskLocked maps a zero-row update to ErrTaskNotFound when the task is gone. Caller holds d.mu.
func (d *DB) requireTaskLocked(id string, rowsAffected int64) error {
	if rowsAffected > 0 {
		return nil
	}
	var exists int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	return nil
}
