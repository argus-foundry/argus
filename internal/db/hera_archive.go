package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// ArchiveRefusedError is a done-done refusal discovered INSIDE the archive
// transaction. Nothing was written (the whole transaction rolled back).
type ArchiveRefusedError struct{ Reason string }

func (e *ArchiveRefusedError) Error() string { return "archive refused: " + e.Reason }

func archiveRefuse(format string, a ...any) error {
	return &ArchiveRefusedError{Reason: fmt.Sprintf(format, a...)}
}

// maxFyiBody mirrors validBoardContent's body cap (64 KiB).
const maxFyiBody = 64 * 1024

// archiveUnitStepHook is a test seam: it runs after each named write step
// inside the transaction, and an error rolls the whole unit back.
var archiveUnitStepHook func(step string) error

// TruncateUTF8 cuts s to at most max bytes without splitting a rune. A
// non-positive max yields "" and the loop terminates on an empty string.
func TruncateUTF8(s string, max int) string {
	if max <= 0 {
		return ""
	}
	for len(s) > max {
		_, size := utf8.DecodeLastRuneInString(s)
		if size == 0 {
			return ""
		}
		s = s[:len(s)-size]
	}
	return s
}

// ArchiveUnitInput names the role to archive and the last-step hook.
type ArchiveUnitInput struct {
	RoleID int64
	// Kind selects which DB-side clauses apply (design D1): merged (default,
	// zero value) needs a complete task; handed_off accepts in_review or
	// complete; forced skips every soft clause and requires Reason.
	Kind ArchiveKind
	// Reason is the mandatory forced-archive justification (stored on the
	// record and echoed in the mail digest).
	Reason string
	// PRURL and TipSHA are recorded for a handed_off archive.
	PRURL, TipSHA string
	// ActorRoleID is the coordinator performing the archive (0 = unknown).
	ActorRoleID int64
	// PostCommit runs after commit with the forwarded mail digest (nil when no
	// mail was forwarded); the caller rings the coordinator doorbell.
	PostCommit func(digest *HeraMessage)
	// PreCommit runs as the very last step inside the transaction, after every
	// write (cheap checks only: it executes under the database lock). A non-nil
	// error rolls the whole unit back; return an *ArchiveRefusedError to surface
	// a reason.
	PreCommit func(taskID string) error
}

// ArchiveHeraUnit is the single-transaction done-done archive
// (add-hera-auto-archive). Inside ONE transaction it re-verifies every
// DB-side clause (task complete, not pinned/resident/standing-team, role not
// blocked, no other live role on the task except a bridged child-coordinator
// hat, no unread mail in or out, no live plan dependents, child roles
// archived, no open decision items), then performs every write: re-post the
// sub-coordinator's open fyi board items to the parent coordinator's board,
// end the child coordinator binding(s), archive the child orchestrator(s),
// archive the task, hide the role, and finally runs PreCommit. Any failure
// rolls EVERYTHING back. Events are emitted only after commit. Returns the
// archived task ID.
func (d *DB) ArchiveHeraUnit(in ArchiveUnitInput) (string, error) {
	if in.Kind == "" {
		in.Kind = ArchiveKindMerged
	}
	if in.Kind == ArchiveKindForced && strings.TrimSpace(in.Reason) == "" {
		return "", archiveRefuse("forced archive requires a non-empty reason")
	}
	var (
		taskID                 string
		oldArchived, oldPinned int
		postedParent, resolved []boardTouch
		digest                 *HeraMessage
	)
	err := d.WithTx(func(tx *sql.Tx) error {
		// Take the write lock before reading (see heraRoleMutation).
		if _, err := tx.Exec(`UPDATE hera_roles SET id=id WHERE id=?`, in.RoleID); err != nil {
			return fmt.Errorf("archive unit: %w", err)
		}
		role, err := scanHeraRole(tx.QueryRow(
			`SELECT id, orchestrator_id, name, kind, argus_project, prompt, created_at, archived_at, pinned_at, nuked_at, node_kind, cancelled_at, archetype
			 FROM hera_roles WHERE id=?`, in.RoleID))
		if err != nil {
			return err
		}
		switch {
		case role.Kind == HeraKindCoordinator:
			return archiveRefuse("coordinator roles are archived by the human or the parent coordinator")
		case role.ArchivedAt != nil:
			return archiveRefuse("role already archived")
		case role.PinnedAt != nil:
			return archiveRefuse("role is pinned")
		case role.NukedAt != nil || role.CancelledAt != nil:
			return archiveRefuse("role is nuked or cancelled")
		}
		if err := tx.QueryRow(`SELECT argus_task_id FROM hera_bindings WHERE role_id=? AND ended_at IS NULL`, in.RoleID).Scan(&taskID); err != nil {
			return archiveRefuse("no live binding")
		}
		if err := d.archiveUnitChecks(tx, role, taskID, in.Kind); err != nil {
			return err
		}
		kids, err := archiveChildOrchs(tx, role, taskID)
		if err != nil {
			return err
		}
		if err := archiveUnitKidChecks(tx, kids, in.Kind == ArchiveKindForced); err != nil {
			return err
		}
		allowed := map[int64]bool{}
		for _, k := range kids {
			allowed[k] = true
		}
		if err := archiveUnitBindingCheck(tx, in.RoleID, taskID, allowed); err != nil {
			return err
		}

		// ---- writes ----
		digest, err = archiveForwardMail(tx, role, in)
		if err != nil {
			return err
		}
		if err := archiveStep("mail"); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hera_messages SET read_at=? WHERE to_role_id=? AND read_at IS NULL
		 AND EXISTS (SELECT 1 FROM hera_merge_notices n WHERE n.message_id=hera_messages.id)`, formatTime(time.Now()), role.ID); err != nil {
			return fmt.Errorf("retire merge notices: %w", err)
		}
		if err := archiveStep("merge_notices"); err != nil {
			return err
		}
		postedParent, resolved, err = archivePushFyiUp(tx, role, kids)
		if err != nil {
			return fmt.Errorf("push fyi to parent: %w", err)
		}
		if err := archiveStep("fyi"); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hera_bindings SET ended_at=?, end_reason=?
			WHERE argus_task_id=? AND ended_at IS NULL AND role_id IN (SELECT id FROM hera_roles WHERE kind=?)`,
			formatTime(time.Now()), HeraEndReasonAccepted, taskID, string(HeraKindCoordinator)); err != nil {
			return fmt.Errorf("end coordinator bindings: %w", err)
		}
		// Link-only children (TUI adopt) have a coordinator on a DIFFERENT task:
		// end those hats too, so no live coordinator outlives its archived orchestrator.
		for _, k := range kids {
			if _, err := tx.Exec(`UPDATE hera_bindings SET ended_at=?, end_reason=?
				WHERE ended_at IS NULL AND orchestrator_id=? AND role_id IN (SELECT id FROM hera_roles WHERE kind=?)`,
				formatTime(time.Now()), HeraEndReasonAccepted, k, string(HeraKindCoordinator)); err != nil {
				return fmt.Errorf("end child orchestrator %d coordinator bindings: %w", k, err)
			}
		}
		if err := archiveStep("end_hat"); err != nil {
			return err
		}
		for _, k := range kids {
			if _, err := tx.Exec(`UPDATE hera_orchestrators SET archived_at=?, pinned_at=NULL WHERE id=? AND archived_at IS NULL`, formatTime(time.Now()), k); err != nil {
				return fmt.Errorf("archive child orchestrator %d: %w", k, err)
			}
		}
		if err := archiveStep("child_orch"); err != nil {
			return err
		}
		oldArchived, oldPinned, err = setArchivedTx(tx, taskID, true)
		if err != nil {
			return fmt.Errorf("archive task: %w", err)
		}
		if err := archiveStep("task"); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hera_roles SET archived_at=?, pinned_at=NULL WHERE id=? AND archived_at IS NULL`, formatTime(time.Now()), in.RoleID); err != nil {
			return fmt.Errorf("hide role: %w", err)
		}
		if err := archiveStep("role"); err != nil {
			return err
		}
		if err := insertArchiveRecord(tx, ArchiveRecord{RoleID: in.RoleID, TaskID: taskID, Kind: in.Kind,
			Reason: strings.TrimSpace(in.Reason), PRURL: in.PRURL, TipSHA: in.TipSHA, ActorRoleID: in.ActorRoleID}); err != nil {
			return err
		}
		if err := archiveStep("record"); err != nil {
			return err
		}
		if in.PreCommit != nil {
			return in.PreCommit(taskID)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	emitArchivedEvents(taskID, true, oldArchived, oldPinned)
	for _, b := range postedParent {
		d.emitBoardChanged(b.roleID, b.itemID, "posted")
	}
	for _, b := range resolved {
		d.emitBoardChanged(b.roleID, b.itemID, "resolved")
	}
	uxlog.Log("[hera] archive unit committed role=%d task=%s kind=%s", in.RoleID, taskID, in.Kind)
	if in.PostCommit != nil {
		in.PostCommit(digest)
	}
	return taskID, nil
}

type boardTouch struct{ roleID, itemID int64 }

func archiveStep(step string) error {
	if archiveUnitStepHook != nil {
		return archiveUnitStepHook(step)
	}
	return nil
}

func (d *DB) archiveUnitChecks(tx *sql.Tx, role *HeraRole, taskID string, mode ArchiveKind) error {
	forced := mode == ArchiveKindForced
	var status, kind string
	var pinned int
	if err := tx.QueryRow(`SELECT status, pinned, kind FROM tasks WHERE id=?`, taskID).Scan(&status, &pinned, &kind); err != nil {
		return archiveRefuse("task not loadable (%v)", err)
	}
	switch {
	case forced:
	case mode == ArchiveKindHandedOff:
		if status != "complete" && status != "in_review" {
			return archiveRefuse("task status is %s, not in_review or complete", status)
		}
	case status != "complete":
		return archiveRefuse("task status is %s, not complete", status)
	}
	if pinned != 0 || kind == "resident" {
		return archiveRefuse("task is resident or pinned")
	}
	var n int
	if err := tx.QueryRow(`SELECT (EXISTS(SELECT 1 FROM task_meta WHERE task_id=? AND namespace='hera' AND key='resident_worker' AND value='true') OR EXISTS(SELECT 1 FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id JOIN hera_orchestrators o ON o.id=b.orchestrator_id WHERE b.argus_task_id=? AND b.ended_at IS NULL AND r.kind='worker' AND o.name='resident' AND EXISTS (SELECT 1 FROM hera_bindings rb JOIN tasks rt ON rt.id=rb.argus_task_id WHERE rb.orchestrator_id=o.id AND rb.ended_at IS NULL AND rt.kind='resident')))`, taskID, taskID).Scan(&n); err != nil {
		return archiveRefuse("standing-team membership unverifiable (%v)", err)
	}
	if n > 0 {
		return archiveRefuse("standing-team member")
	}
	if forced {
		return nil // hard refusals above stay; every soft clause below is overridden
	}
	var st string
	switch err := tx.QueryRow(`SELECT status FROM hera_role_status WHERE role_id=?`, role.ID).Scan(&st); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return archiveRefuse("role status unreadable (%v)", err)
	case st == string(HeraStatusBlocked):
		return archiveRefuse("role is blocked (needs input)")
	}
	// Unread mail TO the role no longer blocks: archiveForwardMail digests it
	// to the coordinator inside this transaction.
	if err := tx.QueryRow(`SELECT COUNT(*) FROM hera_messages m JOIN hera_roles r ON r.id=m.to_role_id
		WHERE m.from_role_id=? AND m.read_at IS NULL AND r.archived_at IS NULL AND r.nuked_at IS NULL
		  AND EXISTS (SELECT 1 FROM hera_bindings b WHERE b.role_id=r.id AND b.ended_at IS NULL)`, role.ID).Scan(&n); err != nil {
		return archiveRefuse("outbox unreadable (%v)", err)
	}
	if n > 0 {
		return archiveRefuse("%d message(s) from this role not yet read by their recipient", n)
	}
	var dep string
	err := tx.QueryRow(`SELECT blocked.name FROM hera_blocks bl
		JOIN hera_roles blocked ON blocked.id=bl.blocked_role_id
		WHERE bl.blocker_role_id=? AND blocked.archived_at IS NULL AND blocked.nuked_at IS NULL AND blocked.cancelled_at IS NULL
		  AND COALESCE((SELECT status FROM hera_role_status WHERE role_id=blocked.id),'') <> 'done' LIMIT 1`, role.ID).Scan(&dep)
	switch {
	case err == nil:
		return archiveRefuse("plan node %q still depends on this role", dep)
	case !errors.Is(err, sql.ErrNoRows):
		return archiveRefuse("plan edges unreadable (%v)", err)
	}
	return nil
}

// archiveChildOrchs mirrors hera.childOrchs: explicit links plus bridged
// coordinator hats (live or accepted-ended) on the same task.
func archiveChildOrchs(tx *sql.Tx, role *HeraRole, taskID string) ([]int64, error) {
	rows, err := tx.Query(`SELECT child_orchestrator_id FROM hera_orch_links WHERE parent_role_id=?
		UNION
		SELECT b.orchestrator_id FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id
		 WHERE b.argus_task_id=? AND r.kind='coordinator' AND b.orchestrator_id<>?
		   AND (b.ended_at IS NULL OR b.end_reason=?)`, role.ID, taskID, role.OrchestratorID, HeraEndReasonAccepted)
	if err != nil {
		return nil, archiveRefuse("orchestrator links unreadable (%v)", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func archiveUnitKidChecks(tx *sql.Tx, kids []int64, forced bool) error {
	for _, k := range kids {
		var orchName string
		if err := tx.QueryRow(`SELECT name FROM hera_orchestrators WHERE id=?`, k).Scan(&orchName); err == nil && orchName == ResidentOrchestratorName {
			return archiveRefuse("child orchestrator is the standing team")
		}
		if forced {
			continue // forced: live child roles / open decisions are overridden
		}
		var name string
		err := tx.QueryRow(`SELECT name FROM hera_roles WHERE orchestrator_id=? AND kind<>'coordinator'
			AND archived_at IS NULL AND nuked_at IS NULL AND cancelled_at IS NULL LIMIT 1`, k).Scan(&name)
		switch {
		case err == nil:
			return archiveRefuse("child role %q is not archived", name)
		case !errors.Is(err, sql.ErrNoRows):
			return archiveRefuse("child roles unreadable (%v)", err)
		}
		var subj, kind string
		err = tx.QueryRow(`SELECT i.subject,i.kind FROM hera_board_items i JOIN hera_roles r ON r.id=i.role_id
			WHERE r.orchestrator_id=? AND r.kind='coordinator' AND r.archived_at IS NULL AND r.nuked_at IS NULL AND r.cancelled_at IS NULL
			  AND i.kind IN ('decision','action','todo') AND i.resolved=0 LIMIT 1`, k).Scan(&subj, &kind)
		switch {
		case err == nil:
			return archiveRefuse("open %s board item %q", kind, subj)
		case !errors.Is(err, sql.ErrNoRows):
			return archiveRefuse("board unreadable (%v)", err)
		}
	}
	return nil
}

func archiveUnitBindingCheck(tx *sql.Tx, roleID int64, taskID string, bridged map[int64]bool) error {
	rows, err := tx.Query(`SELECT b.role_id, b.orchestrator_id, r.kind FROM hera_bindings b JOIN hera_roles r ON r.id=b.role_id
		WHERE b.argus_task_id=? AND b.ended_at IS NULL`, taskID)
	if err != nil {
		return archiveRefuse("task bindings unreadable (%v)", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rid, oid int64
		var kind string
		if err := rows.Scan(&rid, &oid, &kind); err != nil {
			return err
		}
		if rid != roleID && (!bridged[oid] || kind != string(HeraKindCoordinator)) {
			return archiveRefuse("task is also bound to another live role (%d)", rid)
		}
	}
	return rows.Err()
}

// archivePushFyiUp re-posts each open fyi item on a child orchestrator's
// coordinator board to the parent orchestrator's first coordinator, then
// resolves the original. Idempotent via a body marker.
func archivePushFyiUp(tx *sql.Tx, role *HeraRole, kids []int64) (posted, resolved []boardTouch, err error) {
	var parentID int64
	perr := tx.QueryRow(`SELECT id FROM hera_roles WHERE orchestrator_id=? AND kind='coordinator' AND archived_at IS NULL ORDER BY name ASC LIMIT 1`, role.OrchestratorID).Scan(&parentID)
	hasParent := perr == nil
	if perr != nil && !errors.Is(perr, sql.ErrNoRows) {
		return nil, nil, perr
	}
	for _, k := range kids {
		rows, qerr := tx.Query(`SELECT i.id, i.role_id, i.subject, i.body FROM hera_board_items i JOIN hera_roles r ON r.id=i.role_id
			WHERE r.orchestrator_id=? AND r.kind='coordinator' AND i.kind='fyi' AND i.resolved=0 ORDER BY i.id`, k)
		if qerr != nil {
			return nil, nil, qerr
		}
		type item struct {
			id, roleID    int64
			subject, body string
		}
		var items []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.roleID, &it.subject, &it.body); err != nil {
				rows.Close()
				return nil, nil, err
			}
			items = append(items, it)
		}
		rows.Close()
		for _, it := range items {
			if !hasParent {
				return nil, nil, errors.New("no parent coordinator to receive fyi item")
			}
			marker := fmt.Sprintf("— moved from %s fyi #%d", role.Name, it.id)
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM hera_board_items WHERE role_id=? AND length(body) >= length(?) AND substr(body, -length(?)) = ?`, parentID, marker, marker, marker).Scan(&exists); err != nil {
				return nil, nil, err
			}
			now := formatTime(time.Now())
			if exists == 0 {
				subject := TruncateUTF8("["+role.Name+"] "+it.subject, 240)
				body := TruncateUTF8(it.body, maxFyiBody-len(marker)-2) + "\n\n" + marker
				if !validBoardContent("fyi", subject, body) {
					return nil, nil, ErrHeraBoardInvalid
				}
				res, err := tx.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,created_at,updated_at) VALUES(?,?,?,?,?,?)`, parentID, "fyi", subject, body, now, now)
				if err != nil {
					return nil, nil, err
				}
				id, _ := res.LastInsertId()
				posted = append(posted, boardTouch{parentID, id})
			}
			if _, err := tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by=?,resolution_note=?,updated_at=? WHERE id=?`, "agent", "pushed to parent coordinator on archive", now, it.id); err != nil {
				return nil, nil, err
			}
			resolved = append(resolved, boardTouch{it.roleID, it.id})
		}
	}
	return posted, resolved, nil
}

// maxDigestLine bounds one forwarded message's first line.
const maxDigestLine = 200

// archiveForwardMail marks the role's unread (non-merge-notice) hera mail read
// and forwards ONE digest to the orchestrator's coordinator, all inside the
// archive transaction (design D6). A digest that cannot be stored refuses the
// archive, so mail is never marked read without its summary being delivered.
func archiveForwardMail(tx *sql.Tx, role *HeraRole, in ArchiveUnitInput) (*HeraMessage, error) {
	rows, err := tx.Query(`SELECT m.id, COALESCE(fr.name,''), m.body FROM hera_messages m
		LEFT JOIN hera_roles fr ON fr.id=m.from_role_id
		WHERE m.to_role_id=? AND m.read_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM hera_merge_notices n WHERE n.message_id=m.id) ORDER BY m.id`, role.ID)
	if err != nil {
		return nil, archiveRefuse("inbox unreadable (%v)", err)
	}
	var lines []string
	for rows.Next() {
		var id int64
		var from, body string
		if err := rows.Scan(&id, &from, &body); err != nil {
			rows.Close()
			return nil, archiveRefuse("inbox unreadable (%v)", err)
		}
		first := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
		lines = append(lines, fmt.Sprintf("- #%d from %s: %s", id, from, TruncateUTF8(first, maxDigestLine)))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, archiveRefuse("inbox unreadable (%v)", err)
	}
	if len(lines) == 0 {
		return nil, nil // nothing to forward (a forced reason lives on the record)
	}
	coord, err := coordinatorForOrch(tx, role.OrchestratorID)
	if err != nil {
		return nil, archiveRefuse("coordinator unreadable (%v)", err)
	}
	if coord == 0 {
		return nil, archiveRefuse("%d unread message(s) cannot be forwarded: no live coordinator", len(lines))
	}
	body := fmt.Sprintf("Role %s was archived (%s) with %d unread message(s); marked read. Full bodies: hera_get_messages.\n", role.Name, in.Kind, len(lines))
	if in.Kind == ArchiveKindForced {
		body += "Forced archive reason: " + strings.TrimSpace(in.Reason) + "\n"
	}
	body += strings.Join(lines, "\n")
	tldr := TruncateUTF8(fmt.Sprintf("archived %s: %d unread message(s) forwarded", role.Name, len(lines)), HeraMaxTldrLen)
	msg, err := insertHeraMessage(tx, role.ID, coord, body, tldr, nil)
	if err != nil {
		return nil, archiveRefuse("cannot forward unread mail to the coordinator (%v)", err)
	}
	if _, err := tx.Exec(`UPDATE hera_messages SET read_at=? WHERE to_role_id=? AND read_at IS NULL AND id<>?
		AND NOT EXISTS (SELECT 1 FROM hera_merge_notices n WHERE n.message_id=hera_messages.id)`, formatTime(time.Now()), role.ID, msg.ID); err != nil {
		return nil, fmt.Errorf("mark mail read: %w", err)
	}
	return msg, nil
}
