package db

import (
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// ErrGraduationPending is returned by SetArchived(archived=true) and Delete
// while a graduated worker's restart is still pending (task_meta
// hera/graduation_pending_restart). Archiving or deleting would wipe that
// intent and the orientation outbox, leaving a bypass-active session running
// as a top-level coordinator. The refused call changes nothing.
var ErrGraduationPending = errors.New("graduation restart pending — try again after it restarts")

// graduationPendingGuard returns ErrGraduationPending (wrapped with the task
// id) when id still holds the pending-restart marker. Callers run it inside
// the same transaction as the mutation it protects.
func graduationPendingGuard(q rowQuerier, id string) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`,
		id, HeraMetaKeyGraduationPendingRestart).Scan(&n); err != nil || n == 0 {
		return nil
	}
	uxlog.Log("[resident] refused archive/delete of task %s: graduation pending", id)
	return fmt.Errorf("%w (task %s)", ErrGraduationPending, id)
}

// GraduationPending reports whether taskID still has a graduation restart
// pending. Surfaces use it to refuse a destructive flow BEFORE doing any
// side-effecting work (stopping sessions, removing worktrees).
func (d *DB) GraduationPending(taskID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`,
		taskID, HeraMetaKeyGraduationPendingRestart).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
