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
	if n == 0 {
		var exists int
		if qerr := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id=?`, id).Scan(&exists); qerr != nil {
			return false, qerr
		}
		if exists == 0 {
			return false, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
	}
	return n > 0, nil
}
