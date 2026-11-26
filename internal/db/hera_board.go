package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

var (
	ErrHeraBoardInvalid  = errors.New("board: invalid item or coordinator")
	ErrHeraBoardNotFound = errors.New("board: open item not found for coordinator")
	ErrHeraBoardConflict = errors.New("board: item changed since expected revision")
	ErrHeraBoardResolved = errors.New("board: item already resolved")
)

// HeraBoardItem is a curated human attention item, owned by a durable role.
type HeraBoardItem struct {
	Blocking         bool                `json:"blocking"`
	Trigger          string              `json:"trigger"`
	TriggerPending   bool                `json:"trigger_pending"`
	Command          string              `json:"command"`
	Structured       *HeraBoardStructure `json:"structured"`
	ResolutionChoice string              `json:"resolution_choice"`
	ID               int64               `json:"id"`
	RoleID           int64               `json:"role_id"`
	Kind             string              `json:"kind"`
	Subject          string              `json:"subject"`
	Body             string              `json:"body"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Read             bool                `json:"read"`
	Resolved         bool                `json:"resolved"`
	ResolvedBy       string              `json:"resolved_by"`
	ResolutionNote   string              `json:"resolution_note"`
	ResidentApproval bool                `json:"resident_approval,omitempty"`
	// ApprovalState is the resident approval state (pending, approved,
	// consumed, denied, stale, expired, withdrawn, superseded); empty for an
	// ordinary item.
	ApprovalState string `json:"approval_state,omitempty"`
}

const boardColumns = `id, role_id, kind, subject, body, created_at, updated_at, is_read, resolved, resolved_by, resolution_note, structured, resolution_choice, COALESCE((SELECT a.state FROM resident_approvals a WHERE a.board_id=hera_board_items.id),''), blocking, trigger, trigger_pending, command`

func validBoardContent(kind, subject, body string) bool {
	return (kind == "decision" || kind == "fyi" || kind == "action" || kind == "todo") && strings.TrimSpace(subject) != "" && len(subject) <= 240 && !strings.ContainsFunc(subject, unicode.IsControl) && len(body) <= 64*1024
}

func scanBoard(s rowScanner) (*HeraBoardItem, error) {
	var i HeraBoardItem
	var created, updated, structured string
	err := s.Scan(&i.ID, &i.RoleID, &i.Kind, &i.Subject, &i.Body, &created, &updated, &i.Read, &i.Resolved, &i.ResolvedBy, &i.ResolutionNote, &structured, &i.ResolutionChoice, &i.ApprovalState, &i.Blocking, &i.Trigger, &i.TriggerPending, &i.Command)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHeraBoardNotFound
	}
	if err != nil {
		return nil, err
	}
	if structured != "" {
		if err := json.Unmarshal([]byte(structured), &i.Structured); err != nil {
			return nil, err
		}
	}
	i.CreatedAt = parseTime(created)
	i.UpdatedAt = parseTime(updated)
	i.ResidentApproval = i.ApprovalState != ""
	return &i, nil
}

func (d *DB) PostHeraBoardItem(roleID int64, kind, subject, body string, structure ...*HeraBoardStructure) (*HeraBoardItem, error) {
	i, err := d.postHeraBoardItem(roleID, kind, subject, body, HeraBoardMetadataPatch{}, structure...)
	if err == nil {
		d.emitBoardChanged(roleID, i.ID, "posted")
	}
	return i, err
}

func (d *DB) postHeraBoardItem(roleID int64, kind, subject, body string, patch HeraBoardMetadataPatch, structure ...*HeraBoardStructure) (*HeraBoardItem, error) {
	if !validBoardContent(kind, subject, body) {
		return nil, ErrHeraBoardInvalid
	}
	raw, err := encodeBoardStructure(kind, firstBoardStructure(structure))
	if err != nil {
		return nil, err
	}
	m, err := boardMetadata(kind, nil, patch)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := formatTime(time.Now())
	res, err := d.conn.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,structured,created_at,updated_at,blocking,trigger,trigger_pending,command)
 SELECT id,?,?,?,?,?,?,?,?,?,? FROM hera_roles WHERE id=? AND kind='coordinator' AND archived_at IS NULL`, kind, subject, body, raw, now, now, m.Blocking, m.Trigger, m.TriggerPending, m.Command, roleID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrHeraBoardInvalid
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	uxlog.Log("[board] posted role=%d item=%d kind=%s", roleID, id, kind)
	return scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE id=?`, id))
}

// PostOrRefreshHeraBoardItem is for repeating system notices: when the role
// already has an open, non-approval item with the same kind and subject, it
// replaces that item's body, bumps updated_at and marks it unread instead of
// posting a duplicate. Otherwise it posts a new item.
func (d *DB) PostOrRefreshHeraBoardItem(roleID int64, kind, subject, body string) (*HeraBoardItem, error) {
	if !validBoardContent(kind, subject, body) {
		return nil, ErrHeraBoardInvalid
	}
	d.mu.Lock()
	var id int64
	err := d.conn.QueryRow(`SELECT id FROM hera_board_items WHERE role_id=? AND kind=? AND subject=? AND resolved=0
 AND NOT EXISTS(SELECT 1 FROM resident_approvals a WHERE a.board_id=hera_board_items.id) ORDER BY id DESC LIMIT 1`, roleID, kind, subject).Scan(&id)
	d.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		return d.PostHeraBoardItem(roleID, kind, subject, body)
	}
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	_, err = d.conn.Exec(`UPDATE hera_board_items SET body=?,is_read=0,updated_at=? WHERE id=? AND resolved=0`, body, formatTime(time.Now()), id)
	var i *HeraBoardItem
	if err == nil {
		i, err = scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE id=?`, id))
	}
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	uxlog.Log("[board] refreshed role=%d item=%d kind=%s", roleID, id, kind)
	d.emitBoardChanged(roleID, id, "updated")
	return i, nil
}

func (d *DB) UpdateHeraBoardItem(roleID, id int64, kind, subject, body string, structure ...*HeraBoardStructure) (*HeraBoardItem, error) {
	i, changed, err := d.updateHeraBoardItem(roleID, id, kind, subject, body, HeraBoardMetadataPatch{}, structure...)
	if err == nil && changed {
		d.emitBoardChanged(roleID, id, "updated")
	}
	return i, err
}

func (d *DB) updateHeraBoardItem(roleID, id int64, kind, subject, body string, patch HeraBoardMetadataPatch, structure ...*HeraBoardStructure) (*HeraBoardItem, bool, error) {
	if !validBoardContent(kind, subject, body) {
		return nil, false, ErrHeraBoardInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	before, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=? AND resolved=0`, roleID, id))
	if err != nil {
		return nil, false, err
	}
	next := before.Structured
	if kind != before.Kind {
		next = nil
	}
	if len(structure) > 0 {
		next = structure[0]
	}
	raw, err := encodeBoardStructure(kind, next)
	if err != nil {
		return nil, false, err
	}
	old, _ := encodeBoardStructure(before.Kind, before.Structured)
	m, err := boardMetadata(kind, before, patch)
	if err != nil {
		return nil, false, err
	}
	// Readiness changes attention/order, but not the authored content Aaron
	// reviewed. Keep frozen responses and receipt fingerprints valid across it.
	revised := before.Kind != kind || before.Subject != subject || before.Body != body || old != raw || m.Blocking != before.Blocking || m.Trigger != before.Trigger || m.Command != before.Command
	changed := revised || m.TriggerPending != before.TriggerPending
	if before.ResidentApproval {
		return nil, false, ErrResidentApproval
	}
	res, err := d.conn.Exec(`UPDATE hera_board_items SET is_read=CASE WHEN ? THEN 0 ELSE is_read END, updated_at=CASE WHEN ? THEN ? ELSE updated_at END, kind=?,subject=?,body=?,structured=?,blocking=?,trigger=?,trigger_pending=?,command=? WHERE role_id=? AND id=? AND resolved=0`, changed, revised, formatTime(nextBoardRevision(before.UpdatedAt)), kind, subject, body, raw, m.Blocking, m.Trigger, m.TriggerPending, m.Command, roleID, id)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, ErrHeraBoardNotFound
	}
	uxlog.Log("[board] updated role=%d item=%d changed=%v revised=%v", roleID, id, changed, revised)
	i, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
	return i, changed, err
}

func (d *DB) ListHeraBoardItems(roleID int64, includeResolved bool) ([]*HeraBoardItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND (? OR resolved=0) ORDER BY CASE WHEN resolved=0 AND kind='action' AND blocking=1 THEN 0 WHEN trigger_pending=1 OR kind='todo' THEN 2 ELSE 1 END,id`, roleID, includeResolved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*HeraBoardItem, 0)
	for rows.Next() {
		i, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (d *DB) ReadHeraBoardItem(roleID, id int64) error {
	return d.readHeraBoardItem(roleID, id, "")
}

// ReadHeraBoardItemAt acknowledges only the revision the human expanded. A
// material edit racing the async acknowledgment must remain unread.
func (d *DB) ReadHeraBoardItemAt(roleID, id int64, updatedAt time.Time) error {
	return d.readHeraBoardItem(roleID, id, formatTime(updatedAt))
}

func (d *DB) readHeraBoardItem(roleID, id int64, revision string) error {
	changed, err := d.readHeraBoardItemLocked(roleID, id, revision)
	if changed {
		d.emitBoardChanged(roleID, id, "read_state")
	}
	return err
}

func (d *DB) readHeraBoardItemLocked(roleID, id int64, revision string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE hera_board_items SET is_read=1 WHERE role_id=? AND id=? AND resolved=0 AND is_read=0 AND (?='' OR updated_at=?)`, roleID, id, revision, revision)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		if revision != "" {
			uxlog.Log("[board] read skipped stale revision role=%d item=%d", roleID, id)
			return false, nil
		}
		var exists bool
		if err := d.conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM hera_board_items WHERE role_id=? AND id=? AND resolved=0)`, roleID, id).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
		return false, ErrHeraBoardNotFound
	}
	uxlog.Log("[board] read role=%d item=%d", roleID, id)
	return true, nil
}

// SetHeraBoardItemRead sets the read flag of an open item at expected
// revision and returns the item plus the role's unread count. A revision
// mismatch returns ErrHeraBoardConflict and leaves the item untouched; a
// resolved item returns ErrHeraBoardResolved. updated_at is never rewritten.
func (d *DB) SetHeraBoardItemRead(roleID, id int64, read bool, expected time.Time) (*HeraBoardItem, int, error) {
	if expected.IsZero() {
		uxlog.Log("[board] read-state rejected zero revision role=%d item=%d", roleID, id)
		return nil, 0, ErrHeraBoardInvalid
	}
	i, changed, err := d.setHeraBoardItemRead(roleID, id, read, expected)
	if err != nil {
		return nil, 0, err
	}
	if changed {
		d.emitBoardChanged(roleID, id, "read_state")
	}
	n, _, err := d.HeraBoardUnread(roleID)
	return i, n, err
}

func (d *DB) setHeraBoardItemRead(roleID, id int64, read bool, expected time.Time) (*HeraBoardItem, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
	if err != nil {
		return nil, false, err
	}
	if i.Resolved {
		return nil, false, ErrHeraBoardResolved
	}
	if formatTime(i.UpdatedAt) != formatTime(expected) {
		uxlog.Log("[board] read-state conflict role=%d item=%d", roleID, id)
		return nil, false, ErrHeraBoardConflict
	}
	if i.Read == read {
		return i, false, nil
	}
	res, err := d.conn.Exec(`UPDATE hera_board_items SET is_read=? WHERE role_id=? AND id=? AND resolved=0 AND updated_at=?`, read, roleID, id, formatTime(i.UpdatedAt))
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		uxlog.Log("[board] read-state compare failed role=%d item=%d", roleID, id)
		current, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
		if err != nil {
			return nil, false, err
		}
		if current.Resolved {
			return nil, false, ErrHeraBoardResolved
		}
		return nil, false, ErrHeraBoardConflict
	}
	i.Read = read
	uxlog.Log("[board] read-state role=%d item=%d read=%v", roleID, id, read)
	return i, true, nil
}

// DismissHeraBoardItem is the revision-checked human dismissal. A repeat of an
// identical dismissal returns the resolved item with no second notice; a
// changed revision returns ErrHeraBoardConflict instead of dismissing unseen
// content.
func (d *DB) DismissHeraBoardItem(roleID, id int64, expected time.Time, note string) (*HeraBoardItem, int, error) {
	if expected.IsZero() {
		uxlog.Log("[board] dismiss rejected zero revision role=%d item=%d", roleID, id)
		return nil, 0, ErrHeraBoardInvalid
	}
	changed, err := d.resolveHeraBoardItem(roleID, id, "human", note, formatTime(expected))
	if err != nil {
		return nil, 0, err
	}
	if changed {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	d.mu.Lock()
	i, err := scanBoard(d.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
	d.mu.Unlock()
	if err != nil {
		return nil, 0, err
	}
	n, _, err := d.HeraBoardUnread(roleID)
	return i, n, err
}

func (d *DB) emitBoardChanged(roleID, itemID int64, op string) {
	count, _, err := d.HeraBoardUnread(roleID)
	if err != nil {
		uxlog.Log("[board] event unread count failed role=%d: %v", roleID, err)
	}
	var taskID string
	d.mu.Lock()
	_ = d.conn.QueryRow(`SELECT argus_task_id FROM hera_bindings WHERE role_id=? ORDER BY (ended_at IS NULL) DESC, id DESC LIMIT 1`, roleID).Scan(&taskID)
	d.mu.Unlock()
	events.Emit(model.EventTypeHeraBoardChanged, taskID, map[string]any{
		"role_id": roleID, "item_id": itemID, "unread_count": count, "operation": op,
	})
}

// ResolveHeraBoardItem atomically records human decision dismissal in the
// coordinator inbox. This system notice bypasses agent self-send/rate caps;
// it is durable even when no coordinator session is currently bound.
func (d *DB) ResolveHeraBoardItem(roleID, id int64, by, note string) error {
	changed, err := d.resolveHeraBoardItem(roleID, id, by, note, "")
	if changed {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	return err
}

func (d *DB) resolveHeraBoardItem(roleID, id int64, by, note, expected string) (bool, error) {
	if (by != "agent" && by != "human") || len(note) > 64*1024 {
		return false, ErrHeraBoardInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.conn.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	i, err := scanBoard(tx.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
	if err != nil {
		return false, err
	}
	if i.ResidentApproval {
		// Approval items are decided only by the human approve/deny path.
		// The agent may withdraw its own pending item (never a grant);
		// everything else gets the item's real state.
		if by != "agent" {
			return false, residentApprovalStateErrTx(tx, id, time.Now())
		}
		if err := withdrawResidentApprovalTx(tx, id, note, time.Now()); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		uxlog.Log("[resident-approval] withdrawn role=%d item=%d", roleID, id)
		return true, nil
	}
	if i.Resolved {
		if expected == "" || (i.ResolvedBy == by && i.ResolutionNote == note) {
			return false, nil
		}
		return false, ErrHeraBoardResolved
	}
	work := i.Kind == "action" || i.Kind == "todo"
	if work && by != "agent" {
		return false, ErrHeraBoardInvalid
	}
	if expected != "" && formatTime(i.UpdatedAt) != expected {
		uxlog.Log("[board] dismiss conflict role=%d item=%d", roleID, id)
		return false, ErrHeraBoardConflict
	}
	now := formatTime(time.Now())
	choice := ""
	if work {
		choice = "done"
	}
	_, err = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by=?,resolution_note=?,updated_at=?,resolution_choice=? WHERE id=?`, by, note, now, choice, id)
	if err != nil {
		return false, err
	}
	if work {
		i.UpdatedAt = parseTime(now)
		i.ResolvedBy = by
		if err = writeBoardWorkNotice(tx, roleID, i, choice, note); err != nil {
			return false, err
		}
	}
	if by == "human" && i.Kind == "decision" {
		body := fmt.Sprintf("Aaron dropped board decision #%d: %s. Do not act on a stale assumption. Follow up on the board if needed.", id, i.Subject)
		if note != "" {
			body += "\n\n" + note
		}
		_, err = tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,?)`, roleID, roleID, body, fmt.Sprintf("Aaron dropped board decision #%d", id), now, HeraDeliveryPending)
		if err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	uxlog.Log("[board] resolved role=%d item=%d by=%s", roleID, id, by)
	return true, nil
}

// PendingHeraBoardNotices is the durable board outbox. Leave unbound notices
// pending until a binding exists, and never deliver already-read mail.
func (d *DB) PendingHeraBoardNotices() ([]*HeraMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, read_at, delivery_mode, delivered_at
 FROM hera_messages WHERE from_role_id=to_role_id AND read_at IS NULL
 AND delivery_mode IN ('pending','queued_no_binding')
 AND EXISTS (SELECT 1 FROM hera_bindings WHERE role_id=to_role_id AND ended_at IS NULL)
 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*HeraMessage
	for rows.Next() {
		m, err := scanHeraMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// HeraBoardUnread returns a read-only band summary. Some lightweight callers
// have partial schemas predating the board; treat only a missing table as empty.
func (d *DB) HeraBoardUnread(roleID int64) (int, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var count int
	var subject string
	err := d.conn.QueryRow(`SELECT COUNT(*), COALESCE((SELECT subject FROM hera_board_items
 WHERE role_id=? AND resolved=0 AND is_read=0 AND trigger_pending=0 AND kind!='todo' ORDER BY CASE WHEN kind='action' AND blocking=1 THEN 0 ELSE 1 END,updated_at DESC,id DESC LIMIT 1),'')
 FROM hera_board_items WHERE role_id=? AND resolved=0 AND is_read=0 AND trigger_pending=0 AND kind!='todo'`, roleID, roleID).Scan(&count, &subject)
	if err != nil && strings.Contains(err.Error(), "no such table: hera_board_items") {
		uxlog.Log("[board] band summary skipped: board table absent")
		return 0, "", nil
	}
	return count, subject, err
}
