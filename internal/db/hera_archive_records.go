package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ArchiveKind says WHY a role was archived (add-hera-handoff-archive).
type ArchiveKind string

const (
	// ArchiveKindMerged is the original done-done archive: work confirmed landed.
	ArchiveKindMerged ArchiveKind = "merged"
	// ArchiveKindHandedOff archives a role whose PR is open and green; the PR and
	// the batch own the work. The PR poller keeps watching it until it settles.
	ArchiveKindHandedOff ArchiveKind = "handed_off"
	// ArchiveKindForced is the coordinator's escape hatch; Reason is mandatory.
	ArchiveKindForced ArchiveKind = "forced"
)

// ErrHeraRoleLive is the DB backstop: SetArchived(true) / Update refuse to
// archive a task that a live (visible, un-nuked) worker or freelance role is
// still bound to. The hera archive transaction (setArchivedTx) and nuke (which
// ends the binding first) never trip it. Wrapped with the task ID.
var ErrHeraRoleLive = errors.New("task is bound to a live hera worker/freelance role; archive it through hera_archive (force=true with a reason overrides)")

// heraRoleLiveGuard returns ErrHeraRoleLive when taskID still has a live,
// visible worker/freelance binding. Read errors fail CLOSED.
func heraRoleLiveGuard(q rowQuerier, taskID string) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id
		WHERE b.argus_task_id=? AND b.ended_at IS NULL AND r.kind IN ('worker','freelance')
		  AND r.archived_at IS NULL AND r.nuked_at IS NULL AND r.cancelled_at IS NULL`, taskID).Scan(&n); err != nil {
		return fmt.Errorf("hera live-role check for task %s: %w", taskID, err)
	}
	if n > 0 {
		return fmt.Errorf("%w (task %s)", ErrHeraRoleLive, taskID)
	}
	return nil
}

// ArchiveRecord is one row of hera_archive_records.
type ArchiveRecord struct {
	ID          int64
	RoleID      int64
	TaskID      string
	Kind        ArchiveKind
	Reason      string
	PRURL       string
	TipSHA      string
	ActorRoleID int64
	CreatedAt   time.Time
	SettledAt   time.Time // zero while unsettled
	SettleNote  string
}

func insertArchiveRecord(tx *sql.Tx, r ArchiveRecord) error {
	_, err := tx.Exec(`INSERT INTO hera_archive_records(role_id,task_id,kind,reason,pr_url,tip_sha,actor_role_id,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, r.RoleID, r.TaskID, string(r.Kind), r.Reason, r.PRURL, r.TipSHA, r.ActorRoleID, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("insert archive record: %w", err)
	}
	return nil
}

func scanArchiveRecord(s rowScanner) (*ArchiveRecord, error) {
	var r ArchiveRecord
	var kind, created string
	var settled sql.NullString
	if err := s.Scan(&r.ID, &r.RoleID, &r.TaskID, &kind, &r.Reason, &r.PRURL, &r.TipSHA, &r.ActorRoleID, &created, &settled, &r.SettleNote); err != nil {
		return nil, err
	}
	r.Kind = ArchiveKind(kind)
	r.CreatedAt = parseTime(created)
	if settled.Valid {
		r.SettledAt = parseTime(settled.String)
	}
	return &r, nil
}

const archiveRecordColumns = `id, role_id, task_id, kind, reason, pr_url, tip_sha, actor_role_id, created_at, settled_at, settle_note`

// ArchiveRecordsForRole returns a role's archive history, newest first.
func (d *DB) ArchiveRecordsForRole(roleID int64) ([]ArchiveRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT `+archiveRecordColumns+` FROM hera_archive_records WHERE role_id=? ORDER BY id DESC`, roleID)
	if err != nil {
		return nil, fmt.Errorf("archive records: %w", err)
	}
	defer rows.Close()
	var out []ArchiveRecord
	for rows.Next() {
		r, err := scanArchiveRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ArchiveRecordsForTask returns the archive history of every role that was
// bound to taskID, oldest first (the TUI inbox viewer's history source).
func (d *DB) ArchiveRecordsForTask(taskID string) ([]ArchiveRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT `+archiveRecordColumns+` FROM hera_archive_records WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("archive records for task: %w", err)
	}
	defer rows.Close()
	var out []ArchiveRecord
	for rows.Next() {
		r, err := scanArchiveRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// OpenHandoffRecords returns every unsettled handed_off record whose task is
// still archived (the PR poller's extra eligibility set), keyed by task ID.
func (d *DB) OpenHandoffRecords() (map[string]ArchiveRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT ` + archiveRecordColumns + ` FROM hera_archive_records
		WHERE kind='handed_off' AND settled_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("open handoff records: %w", err)
	}
	defer rows.Close()
	out := map[string]ArchiveRecord{}
	for rows.Next() {
		r, err := scanArchiveRecord(rows)
		if err != nil {
			return nil, err
		}
		out[r.TaskID] = *r
	}
	return out, rows.Err()
}

// coordinatorForOrch returns the orchestrator's first visible coordinator role
// (name order, matching archivePushFyiUp), or 0 when none.
func coordinatorForOrch(tx *sql.Tx, orchID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM hera_roles WHERE orchestrator_id=? AND kind='coordinator' AND archived_at IS NULL AND nuked_at IS NULL ORDER BY name ASC LIMIT 1`, orchID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// BoardPost is a board item queued inside a transaction.
type BoardPost struct{ Kind, Subject, Body string }

func postBoardTx(tx *sql.Tx, roleID int64, p BoardPost) (int64, error) {
	p.Subject = TruncateUTF8(strings.ReplaceAll(p.Subject, "\n", " "), 240)
	if !validBoardContent(p.Kind, p.Subject, p.Body) {
		return 0, ErrHeraBoardInvalid
	}
	now := formatTime(time.Now())
	res, err := tx.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		roleID, p.Kind, p.Subject, p.Body, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UnarchiveUnitInput names the role to bring back.
type UnarchiveUnitInput struct {
	RoleID int64
	Note   string     // settle note for an open handed_off record
	Board  *BoardPost // optional item for the orchestrator's coordinator
}

// unarchiveTx is the shared un-archive body: role visible, task un-archived,
// a review-parked/complete task rolled back to in_progress, terminal role
// status reopened, the open handed_off record settled, optional board post.
func unarchiveTx(tx *sql.Tx, in UnarchiveUnitInput) (taskID string, oldArchived, oldPinned int, statusFrom model.Status, boardRole, boardItem int64, err error) {
	if _, err = tx.Exec(`UPDATE hera_roles SET id=id WHERE id=?`, in.RoleID); err != nil {
		return
	}
	role, rerr := scanHeraRole(tx.QueryRow(
		`SELECT id, orchestrator_id, name, kind, argus_project, prompt, created_at, archived_at, pinned_at, nuked_at, node_kind, cancelled_at, archetype
		 FROM hera_roles WHERE id=?`, in.RoleID))
	if rerr != nil {
		err = rerr
		return
	}
	switch {
	case role.Kind == HeraKindCoordinator:
		err = archiveRefuse("coordinator roles are not un-archived here")
	case role.NukedAt != nil || role.CancelledAt != nil:
		err = archiveRefuse("role is nuked or cancelled")
	case role.ArchivedAt == nil:
		err = archiveRefuse("role is not archived")
	}
	if err != nil {
		return
	}
	if qerr := tx.QueryRow(`SELECT argus_task_id FROM hera_bindings WHERE role_id=? AND ended_at IS NULL`, in.RoleID).Scan(&taskID); qerr != nil {
		err = archiveRefuse("no live binding")
		return
	}
	if _, err = tx.Exec(`UPDATE hera_roles SET archived_at=NULL WHERE id=?`, in.RoleID); err != nil {
		return
	}
	if oldArchived, oldPinned, err = setArchivedTx(tx, taskID, false); err != nil {
		return
	}
	var statusStr, startedStr, endedStr string
	if err = tx.QueryRow(`SELECT status, started_at, ended_at FROM tasks WHERE id=?`, taskID).Scan(&statusStr, &startedStr, &endedStr); err != nil {
		return
	}
	statusFrom, _ = model.ParseStatus(statusStr)
	if statusFrom == model.StatusInReview || statusFrom == model.StatusComplete {
		tmp := &model.Task{Status: statusFrom, StartedAt: parseTime(startedStr), EndedAt: parseTime(endedStr)}
		tmp.SetStatus(model.StatusInProgress)
		if _, err = tx.Exec(`UPDATE tasks SET status=?, started_at=?, ended_at=? WHERE id=?`,
			tmp.Status.String(), formatTime(tmp.StartedAt), formatTime(tmp.EndedAt), taskID); err != nil {
			return
		}
	} else {
		statusFrom = -1 // no transition
	}
	if _, err = tx.Exec(`UPDATE hera_role_status SET status='working', updated_at=? WHERE role_id=? AND status IN ('done','failed')`,
		formatTime(time.Now()), in.RoleID); err != nil {
		return
	}
	if _, err = tx.Exec(`UPDATE hera_archive_records SET settled_at=?, settle_note=? WHERE role_id=? AND kind='handed_off' AND settled_at IS NULL`,
		formatTime(time.Now()), in.Note, in.RoleID); err != nil {
		return
	}
	if in.Board != nil {
		coord, cerr := coordinatorForOrch(tx, role.OrchestratorID)
		if cerr != nil {
			err = cerr
			return
		}
		if coord == 0 {
			uxlog.Log("[hera] unarchive role=%d: no coordinator for the board item (skipped)", in.RoleID)
		} else {
			item, perr := postBoardTx(tx, coord, *in.Board)
			if perr != nil {
				err = perr
				return
			}
			boardRole, boardItem = coord, item
		}
	}
	return
}

// UnarchiveHeraUnit un-archives a worker/freelance role in ONE transaction
// (hera_unarchive and the poller's closed-unmerged path). It starts no session
// and checks no git state. Returns the task ID.
func (d *DB) UnarchiveHeraUnit(in UnarchiveUnitInput) (string, error) {
	var (
		taskID                 string
		oldArchived, oldPinned int
		from                   model.Status
		boardRole, boardItem   int64
	)
	err := d.WithTx(func(tx *sql.Tx) error {
		var err error
		taskID, oldArchived, oldPinned, from, boardRole, boardItem, err = unarchiveTx(tx, in)
		return err
	})
	if err != nil {
		return "", err
	}
	d.emitUnarchived(taskID, oldArchived, oldPinned, from, boardRole, boardItem)
	uxlog.Log("[hera] unarchive committed role=%d task=%s", in.RoleID, taskID)
	return taskID, nil
}

func (d *DB) emitUnarchived(taskID string, oldArchived, oldPinned int, from model.Status, boardRole, boardItem int64) {
	emitArchivedEvents(taskID, false, oldArchived, oldPinned)
	if from >= 0 {
		events.Emit(model.EventTypeTaskStatusChanged, taskID, map[string]string{"from": from.String(), "to": model.StatusInProgress.String()})
		events.EmitTaskUpdated(taskID, model.TaskFieldStatus)
	}
	if boardItem != 0 {
		d.emitBoardChanged(boardRole, boardItem, "posted")
	}
}

// HandoffOutcome is how a watched handed-off PR ended.
type HandoffOutcome string

const (
	HandoffMerged HandoffOutcome = "merged"
	HandoffClosed HandoffOutcome = "closed"
)

// SettleHandoff applies the PR poller's verdict on an unsettled handed_off
// archive (design D4) in ONE transaction. merged: settle, advance an
// in_review task to complete, post a coordinator fyi. closed (unmerged):
// un-archive + revive + coordinator decision item. A task with no open record
// is a no-op (false, nil), so a re-poll is idempotent.
func (d *DB) SettleHandoff(taskID string, outcome HandoffOutcome, prURL string) (bool, error) {
	var (
		settled                bool
		oldArchived, oldPinned int
		from                   model.Status = -1
		boardRole, boardItem   int64
		completed              bool
		unarchiveErr           error
	)
	err := d.WithTx(func(tx *sql.Tx) error {
		var roleID int64
		err := tx.QueryRow(`SELECT role_id FROM hera_archive_records WHERE task_id=? AND kind='handed_off' AND settled_at IS NULL ORDER BY id DESC LIMIT 1`, taskID).Scan(&roleID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var name string
		var orchID int64
		if err := tx.QueryRow(`SELECT name, orchestrator_id FROM hera_roles WHERE id=?`, roleID).Scan(&name, &orchID); err != nil {
			return err
		}
		settled = true
		switch outcome {
		case HandoffMerged:
			now := formatTime(time.Now())
			if _, err := tx.Exec(`UPDATE hera_archive_records SET settled_at=?, settle_note=? WHERE role_id=? AND kind='handed_off' AND settled_at IS NULL`, now, "merged", roleID); err != nil {
				return err
			}
			var statusStr, startedStr, endedStr string
			if err := tx.QueryRow(`SELECT status, started_at, ended_at FROM tasks WHERE id=?`, taskID).Scan(&statusStr, &startedStr, &endedStr); err != nil {
				return err
			}
			if statusStr == model.StatusInReview.String() {
				tmp := &model.Task{Status: model.StatusInReview, StartedAt: parseTime(startedStr), EndedAt: parseTime(endedStr)}
				tmp.SetStatus(model.StatusComplete)
				if _, err := tx.Exec(`UPDATE tasks SET status=?, started_at=?, ended_at=? WHERE id=? AND status=?`,
					tmp.Status.String(), formatTime(tmp.StartedAt), formatTime(tmp.EndedAt), taskID, model.StatusInReview.String()); err != nil {
					return err
				}
				completed = true
			}
			coord, err := coordinatorForOrch(tx, orchID)
			if err != nil {
				return err
			}
			if coord != 0 {
				item, err := postBoardTx(tx, coord, BoardPost{Kind: "fyi",
					Subject: "[" + name + "] handed-off PR merged",
					Body:    "The PR for handed-off (archived) role " + name + " merged: " + prURL})
				if err != nil {
					return err
				}
				boardRole, boardItem = coord, item
			}
		case HandoffClosed:
			_, oldArchived, oldPinned, from, boardRole, boardItem, unarchiveErr = unarchiveTx(tx, UnarchiveUnitInput{
				RoleID: roleID, Note: "closed unmerged",
				Board: &BoardPost{Kind: "decision",
					Subject: "[" + name + "] handed-off PR closed without merging",
					Body: "The PR for handed-off role " + name + " was closed WITHOUT merging (" + prURL + "). " +
						"The role was un-archived and its task returned to in_progress so the work is not lost. " +
						"Decide: re-open the PR / re-spawn, or hera_archive it again (force=true with a reason) if it is intentionally abandoned."},
			})
			if unarchiveErr != nil {
				return unarchiveErr // roll the partial un-archive back; the fallback below still tells the coordinator
			}
		default:
			return fmt.Errorf("unknown handoff outcome %q", outcome)
		}
		return nil
	})
	if err != nil && unarchiveErr != nil && errors.Is(err, unarchiveErr) {
		// Revive failed: a revive failure must NOT suppress the coordinator's
		// board item. Settle the record and post the decision on its own.
		uxlog.Log("[hera] handoff close: un-archive failed task=%s: %v (posting board item only)", taskID, unarchiveErr)
		return d.settleClosedBoardOnly(taskID, prURL, unarchiveErr)
	}
	if err != nil {
		return false, err
	}
	if settled {
		switch outcome {
		case HandoffClosed:
			d.emitUnarchived(taskID, oldArchived, oldPinned, from, boardRole, boardItem)
		case HandoffMerged:
			if completed {
				events.Emit(model.EventTypeTaskStatusChanged, taskID, map[string]string{"from": model.StatusInReview.String(), "to": model.StatusComplete.String()})
				events.Emit(model.EventTypeTaskCompleted, taskID, nil)
				events.EmitTaskUpdated(taskID, model.TaskFieldStatus)
			}
			if boardItem != 0 {
				d.emitBoardChanged(boardRole, boardItem, "posted")
			}
		}
		uxlog.Log("[hera] handoff settled task=%s outcome=%s", taskID, outcome)
	}
	return settled, nil
}

// settleClosedBoardOnly is the fallback for a closed-unmerged handoff whose
// un-archive failed: it still settles the record and posts the decision item.
func (d *DB) settleClosedBoardOnly(taskID, prURL string, cause error) (bool, error) {
	var boardRole, boardItem int64
	err := d.WithTx(func(tx *sql.Tx) error {
		var roleID, orchID int64
		var name string
		if err := tx.QueryRow(`SELECT r.id, r.orchestrator_id, r.name FROM hera_archive_records a JOIN hera_roles r ON r.id=a.role_id
			WHERE a.task_id=? AND a.kind='handed_off' AND a.settled_at IS NULL ORDER BY a.id DESC LIMIT 1`, taskID).Scan(&roleID, &orchID, &name); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hera_archive_records SET settled_at=?, settle_note=? WHERE role_id=? AND kind='handed_off' AND settled_at IS NULL`,
			formatTime(time.Now()), TruncateUTF8("closed unmerged; un-archive failed: "+cause.Error(), 500), roleID); err != nil {
			return err
		}
		coord, err := coordinatorForOrch(tx, orchID)
		if err != nil || coord == 0 {
			return err
		}
		item, err := postBoardTx(tx, coord, BoardPost{Kind: "decision",
			Subject: "[" + name + "] handed-off PR closed without merging",
			Body: "The PR for handed-off role " + name + " was closed WITHOUT merging (" + prURL + "). " +
				"Automatically reviving the role failed (" + cause.Error() + "); the role is still archived. Decide what happens to the work."})
		boardRole, boardItem = coord, item
		return err
	})
	if err != nil {
		return false, err
	}
	if boardItem != 0 {
		d.emitBoardChanged(boardRole, boardItem, "posted")
	}
	return true, nil
}
