package db

import (
	"database/sql"
	"fmt"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// SendHeraMergeNotice commits the daemon's self-assessment message and its
// provenance together. A stale poll snapshot must not prompt a finished task.
// A nil message means the task is already complete or archived.
func (d *DB) SendHeraMergeNotice(taskID string, fromRoleID, toRoleID int64, body, tldr string) (*HeraMessage, error) {
	var msg *HeraMessage
	err := d.WithTx(func(tx *sql.Tx) error {
		// Acquire the write lock before reading, serializing completion and archive.
		if _, err := tx.Exec("UPDATE tasks SET id=id WHERE id=?", taskID); err != nil {
			return err
		}
		var status string
		var archived int
		if err := tx.QueryRow("SELECT status,archived FROM tasks WHERE id=?", taskID).Scan(&status, &archived); err != nil {
			return err
		}
		if status == "complete" || archived != 0 {
			uxlog.Log("[pr] poll: skip role merge notice task=%s status=%s archived=%d", taskID, status, archived)
			return nil
		}
		// Include historical bindings supported by poller role resolution.
		var bound int
		if err := tx.QueryRow("SELECT COUNT(*) FROM hera_bindings WHERE role_id=? AND argus_task_id=?", toRoleID, taskID).Scan(&bound); err != nil {
			return err
		}
		if bound == 0 {
			return fmt.Errorf("merge notice: recipient role is not bound to task %s", taskID)
		}
		var err error
		msg, err = insertHeraMessage(tx, fromRoleID, toRoleID, body, tldr, nil)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO hera_merge_notices(message_id) VALUES(?)", msg.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if msg != nil {
		uxlog.Log("[pr] poll: role merge notice stored task=%s role=%d message=%d", taskID, toRoleID, msg.ID)
	}
	return msg, nil
}

// archiveUnreadSQL is shared by preflight and the locked archive check. Never
// classify ordinary coordinator mail by its sender, subject or body.
const archiveUnreadSQL = `SELECT COUNT(*) FROM hera_messages m
 WHERE m.to_role_id=? AND m.read_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM hera_merge_notices n WHERE n.message_id=m.id)`

// HeraArchiveUnread counts actionable mail for a role whose task the caller
// has already verified complete. Normal inbox reads still return merge prompts.
func (d *DB) HeraArchiveUnread(roleID int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	err := d.conn.QueryRow(archiveUnreadSQL, roleID).Scan(&n)
	return n, err
}
