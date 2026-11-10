package db

import (
	"database/sql"
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

// residentNoticeQuerier is the *sql.Tx surface the in-transaction notice
// helpers need (never d.conn: WithTx holds d.mu).
type residentNoticeQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// residentOrchestratorTx reports whether orchestrator orchID is the standing
// resident's: it has a live binding to the resident-kind task.
func residentOrchestratorTx(q residentNoticeQuerier, orchID int64) (bool, error) {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id WHERE b.orchestrator_id=? AND b.ended_at IS NULL AND t.kind=?`, orchID, model.KindResident).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// residentRoleProtectedTx is residentHeraGuard("role", id) inside a
// transaction: the resident's own coordinator role must not be mutated.
func residentRoleProtectedTx(q residentNoticeQuerier, id int64) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM hera_roles r JOIN hera_bindings b ON b.role_id=r.id JOIN tasks t ON t.id=b.argus_task_id WHERE r.id=? AND t.kind=? AND b.ended_at IS NULL`, id, model.KindResident).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrResidentProtected
	}
	return nil
}

// insertResidentNoticeTx queues a notice inside the caller's transaction when r
// is a worker of the standing orchestrator and the actor is not the resident.
// An INSERT failure is returned so the surrounding mutation rolls back.
func insertResidentNoticeTx(q residentNoticeQuerier, r *HeraRole, actor HeraActor, kind ResidentNoticeKind, detail string) error {
	if actor == HeraActorResident || r == nil || r.Kind != HeraKindWorker {
		return nil
	}
	resident, err := residentOrchestratorTx(q, r.OrchestratorID)
	if err != nil {
		return fmt.Errorf("resident notice: %w", err)
	}
	if !resident {
		return nil
	}
	if _, err := q.Exec(`INSERT INTO resident_notices(worker_role_id, kind, detail, created_at) VALUES (?,?,?,?)`,
		r.ID, string(kind), detail, formatTime(time.Now())); err != nil {
		return fmt.Errorf("queue resident notice: %w", err)
	}
	uxlog.Log("[resident] notice queued role=%d kind=%s", r.ID, kind)
	return nil
}

// heraRoleMutation applies one role mutation and its resident notice in a
// single transaction. apply runs against the pre-mutation role and reports
// whether a real transition happened (decided by the UPDATE's RowsAffected, so
// a repeat or a concurrent twin queues nothing) plus the notice detail. A
// missing role is ErrHeraNotFound; the resident's own role is protected.
func (d *DB) heraRoleMutation(id int64, actor HeraActor, kind ResidentNoticeKind,
	apply func(tx *sql.Tx, cur *HeraRole) (changed bool, detail string, err error)) error {
	return d.WithTx(func(tx *sql.Tx) error {
		// Take the write lock before reading: a deferred BEGIN that reads first
		// and upgrades later fails with SQLITE_BUSY_SNAPSHOT when another handle
		// (the TUI and daemon each hold their own) commits in between.
		if _, err := tx.Exec(`UPDATE hera_roles SET id=id WHERE id=?`, id); err != nil {
			return fmt.Errorf("hera role mutation: %w", err)
		}
		if err := residentRoleProtectedTx(tx, id); err != nil {
			return err
		}
		cur, err := scanHeraRole(tx.QueryRow(
			`SELECT id, orchestrator_id, name, kind, argus_project, prompt, created_at, archived_at, pinned_at, nuked_at, node_kind, cancelled_at, archetype
			 FROM hera_roles WHERE id=?`, id))
		if err != nil {
			return err
		}
		changed, detail, err := apply(tx, cur)
		if err != nil || !changed {
			return err
		}
		return insertResidentNoticeTx(tx, cur, actor, kind, detail)
	})
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

// PendingResidentNotices returns queued notices, oldest first. claimed_at is
// ignored (legacy rows claimed by the retired claim protocol are recovered).
func (d *DB) PendingResidentNotices(limit int) ([]ResidentNotice, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id, worker_role_id, kind, detail, created_at FROM resident_notices ORDER BY id ASC LIMIT ?`, limit)
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

// CompleteResidentNotice deletes a notice without delivering it (the
// recipient no longer exists or the notice no longer applies).
func (d *DB) CompleteResidentNotice(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`DELETE FROM resident_notices WHERE id=?`, id)
	return err
}

// DeliverResidentNotice is the crash-safe, exactly-once handoff: in ONE
// transaction it deletes the outbox row and inserts the hera message (same
// validation as SendHeraMessage). If the row is already gone it returns
// (nil, nil) and inserts nothing. Any validation or insert error rolls the
// delete back, leaving the row queued. The caller rings the doorbell for the
// returned committed message.
func (d *DB) DeliverResidentNotice(noticeID, fromRoleID, toRoleID int64, body, tldr string) (*HeraMessage, error) {
	var msg *HeraMessage
	err := d.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM resident_notices WHERE id=?`, noticeID)
		if err != nil {
			return fmt.Errorf("delete resident notice: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		msg, err = insertHeraMessage(tx, fromRoleID, toRoleID, body, tldr, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return msg, nil
}
