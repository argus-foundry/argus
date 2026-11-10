package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

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
// the same transaction as the mutation it protects. It fails CLOSED: a query
// error refuses the mutation, like the neighboring resident guards.
func graduationPendingGuard(q rowQuerier, id string) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`,
		id, HeraMetaKeyGraduationPendingRestart).Scan(&n); err != nil {
		uxlog.Log("[resident] refused archive/delete of task %s: graduation check failed: %v", id, err)
		return fmt.Errorf("graduation pending check for task %s: %w", id, err)
	}
	if n == 0 {
		return nil
	}
	uxlog.Log("[resident] refused archive/delete of task %s: graduation pending", id)
	return fmt.Errorf("%w (task %s)", ErrGraduationPending, id)
}

// GraduationPendingErr reports whether taskID still has a graduation restart
// pending, surfacing a query error so callers can refuse rather than guess.
func (d *DB) GraduationPendingErr(taskID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM task_meta WHERE task_id=? AND namespace='hera' AND key=?`,
		taskID, HeraMetaKeyGraduationPendingRestart).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// GraduationPending is the fail-CLOSED boolean form: an unreadable state is
// reported as pending so destructive flows refuse. Surfaces use it to refuse
// BEFORE doing any side-effecting work (stopping sessions, removing worktrees).
func (d *DB) GraduationPending(taskID string) bool {
	pending, err := d.GraduationPendingErr(taskID)
	if err != nil {
		uxlog.Log("[resident] graduation pending check failed task=%s: %v (treating as pending)", taskID, err)
		return true
	}
	return pending
}

// AbandonGraduationPending is the HUMAN-ONLY escape for a wedged graduation:
// one transaction clears the restart intent, the orientation outbox and the
// recorded graduate-binding id. It must never be wired to MCP or REST.
func (d *DB) AbandonGraduationPending(taskID string) error {
	if err := d.ClearGraduationState(taskID); err != nil {
		return err
	}
	uxlog.Log("[resident] graduation abandoned by human task=%s", taskID)
	return nil
}

// ClearGraduationState removes the pending-restart, orientation-outbox and
// recorded-binding keys in one transaction. The daemon watcher uses it when the
// graduate binding ended; the human escape wraps it with its own log line.
func (d *DB) ClearGraduationState(taskID string) error {
	return d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace='hera' AND key IN (?,?,?)`,
			taskID, HeraMetaKeyGraduationPendingRestart, "graduation_orientation", HeraMetaKeyGraduationBindingID)
		return err
	})
}

// HeraBindingByID returns the binding with the given id whether live or ended,
// or ErrHeraNotFound.
func (d *DB) HeraBindingByID(id int64) (*HeraBinding, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	row := d.conn.QueryRow(
		`SELECT id, role_id, orchestrator_id, argus_task_id, worktree_path, started_at, ended_at, end_reason
		 FROM hera_bindings WHERE id=?`, id)
	return scanHeraBinding(row)
}

// AdvanceGraduationPhase compare-and-sets the pending-restart phase from->to in
// one statement. It reports false (writing nothing) when the key is gone or no
// longer holds from, so the daemon watcher can never re-create a marker that a
// human abandon just cleared.
func (d *DB) AdvanceGraduationPhase(taskID, from, to string) (bool, error) {
	changed := false
	err := d.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE task_meta SET value=?, updated_at=? WHERE task_id=? AND namespace='hera' AND key=? AND value=?`,
			to, formatTime(time.Now()), taskID, HeraMetaKeyGraduationPendingRestart, from)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		changed = n > 0
		return err
	})
	return changed && err == nil, err
}
