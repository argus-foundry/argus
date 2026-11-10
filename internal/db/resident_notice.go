package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// HeraActor names who performs a role mutation, so the resident is told about
// human actions on its workers but never echoed its own. Everything that does
// not say otherwise is the human (rail, REST, Tasks list).
type HeraActor string

const (
	HeraActorHuman    HeraActor = "human"
	HeraActorResident HeraActor = "resident"
)

// ResidentNoticeKind is the action a notice reports.
type ResidentNoticeKind string

const (
	ResidentNoticeNuked   ResidentNoticeKind = "nuked"
	ResidentNoticeHidden  ResidentNoticeKind = "hidden"
	ResidentNoticeRenamed ResidentNoticeKind = "renamed"
)

// ResidentNotice is one queued human-action report about a resident worker.
type ResidentNotice struct {
	ID           int64
	WorkerRoleID int64
	Kind         ResidentNoticeKind
	Detail       string // renamed: "old → new"; nuked via Tasks list: how it happened
	CreatedAt    time.Time
}

// pendingResidentNotice is a notice decided while the role was still in its
// pre-mutation state, queued only once the mutation has succeeded.
type pendingResidentNotice struct {
	roleID int64
	kind   ResidentNoticeKind
	detail string
}

// residentNoticeCandidate reports (before the mutation) whether a human
// nuke/hide of roleID should notify the resident: a worker of the standing
// orchestrator, not already in the target state. Membership is decided by the
// role's orchestrator binding, never task_meta (SetArchived wipes it) or name.
func (d *DB) residentNoticeCandidate(roleID int64, actor HeraActor, kind ResidentNoticeKind) *pendingResidentNotice {
	if actor == HeraActorResident {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.heraRoleByID(roleID)
	if err != nil {
		return nil
	}
	if (kind == ResidentNoticeHidden && r.ArchivedAt != nil) || (kind == ResidentNoticeNuked && r.NukedAt != nil) {
		return nil // idempotent repeat: nothing changes, nothing to report
	}
	return d.residentNoticeLocked(r, actor, kind, "")
}

// residentNoticeLocked builds the pending notice for r when r is a worker of
// the standing orchestrator. Called with d.mu held.
func (d *DB) residentNoticeLocked(r *HeraRole, actor HeraActor, kind ResidentNoticeKind, detail string) *pendingResidentNotice {
	if actor == HeraActorResident || r == nil || r.Kind != HeraKindWorker {
		return nil
	}
	if err := d.residentHeraGuard("orchestrator", r.OrchestratorID); !errors.Is(err, ErrResidentProtected) {
		return nil
	}
	return &pendingResidentNotice{roleID: r.ID, kind: kind, detail: detail}
}

// enqueueResidentNotice records a notice decided by residentNoticeCandidate. A
// nil notice is a no-op; a write failure is logged, never returned — the
// mutation already happened and the notice is advisory.
func (d *DB) enqueueResidentNotice(n *pendingResidentNotice) {
	if n == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.conn.Exec(`INSERT INTO resident_notices(worker_role_id, kind, detail, created_at) VALUES (?,?,?,?)`,
		n.roleID, string(n.kind), n.detail, formatTime(time.Now())); err != nil {
		uxlog.Log("[resident] notice enqueue failed role=%d kind=%s: %v", n.roleID, n.kind, err)
		return
	}
	uxlog.Log("[resident] notice queued role=%d kind=%s", n.roleID, n.kind)
}

// queueResidentTaskDeleteNotices queues a "nuked" notice for every live
// resident-worker binding on taskID. It runs inside the task-delete
// transaction, before the bindings end.
func queueResidentTaskDeleteNotices(tx *sql.Tx, taskID string) error {
	rows, err := tx.Query(`SELECT b.role_id FROM hera_bindings b
		JOIN hera_roles r ON r.id=b.role_id
		WHERE b.argus_task_id=? AND b.ended_at IS NULL AND r.kind='worker'
		AND EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id
			WHERE rb.orchestrator_id=r.orchestrator_id AND rb.ended_at IS NULL AND rt.kind=?)`, taskID, model.KindResident)
	if err != nil {
		return err
	}
	var roleIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		roleIDs = append(roleIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range roleIDs {
		if _, err := tx.Exec(`INSERT INTO resident_notices(worker_role_id, kind, detail, created_at) VALUES (?,?,?,?)`,
			id, string(ResidentNoticeNuked), "its task was deleted from the Tasks list", formatTime(time.Now())); err != nil {
			return fmt.Errorf("queue resident notice: %w", err)
		}
	}
	return nil
}

// PendingResidentNotices returns unclaimed notices, oldest first.
func (d *DB) PendingResidentNotices(limit int) ([]ResidentNotice, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id, worker_role_id, kind, detail, created_at FROM resident_notices WHERE claimed_at IS NULL ORDER BY id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending resident notices: %w", err)
	}
	defer rows.Close()
	var out []ResidentNotice
	for rows.Next() {
		var n ResidentNotice
		var kind, created string
		if err := rows.Scan(&n.ID, &n.WorkerRoleID, &kind, &n.Detail, &created); err != nil {
			return nil, err
		}
		n.Kind = ResidentNoticeKind(kind)
		n.CreatedAt = parseTime(created)
		out = append(out, n)
	}
	return out, rows.Err()
}

// ClaimResidentNotice marks one notice in flight. False means another claimer
// (or a completion) got there first.
func (d *DB) ClaimResidentNotice(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE resident_notices SET claimed_at=? WHERE id=? AND claimed_at IS NULL`, formatTime(time.Now()), id)
	if err != nil {
		return false, fmt.Errorf("claim resident notice: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseResidentNotice returns a claimed notice to the queue after a failed
// delivery so the next pass retries it. Releasing a completed (deleted) notice
// is a harmless no-op.
func (d *DB) ReleaseResidentNotice(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`UPDATE resident_notices SET claimed_at=NULL WHERE id=?`, id)
	return err
}

// CompleteResidentNotice deletes a delivered notice.
func (d *DB) CompleteResidentNotice(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`DELETE FROM resident_notices WHERE id=?`, id)
	return err
}
