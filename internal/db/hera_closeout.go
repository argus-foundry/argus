package db

import (
	"database/sql"
	"errors"
	"time"
)

// HeraCloseoutSnapshot preserves the rows a forced revival may need to undo.
type HeraCloseoutSnapshot struct {
	TaskID         string
	ReadyToClose   string
	HadReady       bool
	RoleStatuses   map[int64]HeraRoleStatusValue
	readyUpdatedAt string
	roleUpdatedAt  map[int64]string
	clearedAt      string
}

// SnapshotAndClearHeraCloseout atomically captures and clears close-out state.
func (d *DB) SnapshotAndClearHeraCloseout(taskID string) (*HeraCloseoutSnapshot, error) {
	snap := &HeraCloseoutSnapshot{TaskID: taskID, RoleStatuses: map[int64]HeraRoleStatusValue{}, roleUpdatedAt: map[int64]string{}}
	err := d.WithTx(func(tx *sql.Tx) error {
		err := tx.QueryRow(`SELECT value, updated_at FROM task_meta WHERE task_id=? AND namespace=? AND key=?`, taskID, HeraMetaNamespace, HeraMetaKeyReadyToClose).Scan(&snap.ReadyToClose, &snap.readyUpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		snap.HadReady = err == nil
		rows, err := tx.Query(`SELECT DISTINCT s.role_id,s.status,s.updated_at FROM hera_role_status s JOIN hera_bindings b ON b.role_id=s.role_id WHERE b.argus_task_id=? AND b.ended_at IS NULL AND s.status IN ('done','failed')`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var id int64
			var st HeraRoleStatusValue
			var at string
			if err := rows.Scan(&id, &st, &at); err != nil {
				return err
			}
			snap.RoleStatuses[id] = st
			snap.roleUpdatedAt[id] = at
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		snap.clearedAt = formatTime(time.Now())
		if _, err := tx.Exec(`INSERT INTO task_meta(task_id,namespace,key,value,updated_at) VALUES(?,?,?,'false',?) ON CONFLICT(task_id,namespace,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, taskID, HeraMetaNamespace, HeraMetaKeyReadyToClose, snap.clearedAt); err != nil {
			return err
		}
		for id := range snap.RoleStatuses {
			if _, err := tx.Exec(`UPDATE hera_role_status SET status='working',updated_at=? WHERE role_id=?`, snap.clearedAt, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// RestoreHeraCloseout leaves newer writes alone and restores original timestamps.
func (d *DB) RestoreHeraCloseout(snap *HeraCloseoutSnapshot) error {
	if snap == nil {
		return nil
	}
	return d.WithTx(func(tx *sql.Tx) error {
		if snap.HadReady {
			if _, err := tx.Exec(`UPDATE task_meta SET value=?,updated_at=? WHERE task_id=? AND namespace=? AND key=? AND value='false' AND updated_at=?`, snap.ReadyToClose, snap.readyUpdatedAt, snap.TaskID, HeraMetaNamespace, HeraMetaKeyReadyToClose, snap.clearedAt); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace=? AND key=? AND value='false' AND updated_at=?`, snap.TaskID, HeraMetaNamespace, HeraMetaKeyReadyToClose, snap.clearedAt); err != nil {
				return err
			}
		}
		for id, st := range snap.RoleStatuses {
			if _, err := tx.Exec(`UPDATE hera_role_status SET status=?,updated_at=? WHERE role_id=? AND status='working' AND updated_at=?`, st, snap.roleUpdatedAt[id], id, snap.clearedAt); err != nil {
				return err
			}
		}
		return nil
	})
}
