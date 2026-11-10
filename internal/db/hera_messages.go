package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/model"
)

// Sentinel errors for the hera_messages store.
var (
	// ErrHeraMessageBodyTooLarge fires when body exceeds the 64 KiB cap.
	ErrHeraMessageBodyTooLarge = errors.New("hera message body exceeds 64 KiB cap")
	// ErrHeraMessageTldrRequired fires when tldr is empty.
	ErrHeraMessageTldrRequired = errors.New("hera message tldr is required")
	// ErrHeraMessageTldrTooLong fires when tldr exceeds 120 characters.
	ErrHeraMessageTldrTooLong = errors.New("hera message tldr exceeds 120-char cap")
	// ErrHeraMessageTldrMultiline fires when tldr contains a newline.
	ErrHeraMessageTldrMultiline = errors.New("hera message tldr must be a single line")
	// ErrHeraMessageSelfSend fires when fromRoleID == toRoleID.
	ErrHeraMessageSelfSend = errors.New("hera message: cannot send to self")
	// ErrHeraMessageInboxFull fires when the recipient already holds >=
	// HeraMaxUnreadPerRole unread messages.
	ErrHeraMessageInboxFull = errors.New("hera message: recipient inbox full (500 unread cap)")
	// ErrHeraMessageRateLimited fires when the sender has emitted >=
	// HeraMaxSendsPerMinute messages in the past 60 s.
	ErrHeraMessageRateLimited = errors.New("hera message: sender rate limit exceeded (50/min)")
	// ErrHeraMessageRecipientInvalid fires when the recipient role does not
	// exist or is archived (FK would catch non-existence but not archived).
	ErrHeraMessageRecipientInvalid = errors.New("hera message: recipient role is missing or archived")
	// ErrHeraMessageInvalidReplyTo fires when in_reply_to names a message that
	// does not exist.
	ErrHeraMessageInvalidReplyTo = errors.New("hera message: in_reply_to references unknown message")
)

// Caps for the hera message store. Kept in sync with the task_messages
// equivalents (model.MaxMessageBodyBytes / MaxUnreadPerRecipient / MaxSendsPerMinute).
const (
	HeraMaxUnreadPerRole  = 500
	HeraMaxSendsPerMinute = 50
	HeraMaxTldrLen        = 120
	heraRateLimitWindow   = time.Minute
)

// Delivery mode constants for hera_messages.delivery_mode.
const (
	HeraDeliveryPending         = "pending"
	HeraDeliveryIdleSubmit      = "idle_submit"
	HeraDeliveryQueuedNoBinding = "queued_no_binding"
)

// HeraMessage is one message on the hera role-addressed bus.
type HeraMessage struct {
	ID           int64
	FromRoleID   int64
	ToRoleID     int64
	Body         string
	Tldr         string
	InReplyTo    *int64
	SentAt       time.Time
	ReadAt       *time.Time
	DeliveryMode string
	DeliveredAt  *time.Time
}

// SenderName labels reserved self-addressed board notices without inventing a
// role. SendHeraMessage forbids ordinary self-send; only board resolution writes
// this system form, atomically with the human's decision.
func (m *HeraMessage) SenderName(roleName string) string {
	if m.FromRoleID == m.ToRoleID {
		return "Aaron (board)"
	}
	return roleName
}

// SendHeraMessage validates and persists a new hera message. Caps applied:
//   - body ≤ 64 KiB (model.MaxMessageBodyBytes)
//   - tldr required, ≤ 120 chars, single line
//   - self-send rejected
//   - recipient role must exist AND be active (not archived)
//   - recipient unread cap: HeraMaxUnreadPerRole (500)
//   - sender rolling 60-second rate cap: HeraMaxSendsPerMinute (50)
//   - in_reply_to, if non-nil, must reference an existing message
//
// All checks run inside the DB lock so concurrent senders can't both squeak
// past the caps. Returns the inserted message on success.
func (d *DB) SendHeraMessage(fromRoleID, toRoleID int64, body, tldr string, inReplyTo *int64) (*HeraMessage, error) {
	// Validate before acquiring the lock — cheap, no DB needed.
	if fromRoleID == toRoleID {
		return nil, ErrHeraMessageSelfSend
	}
	if len(body) > model.MaxMessageBodyBytes {
		return nil, ErrHeraMessageBodyTooLarge
	}
	if tldr == "" {
		return nil, ErrHeraMessageTldrRequired
	}
	if len(tldr) > HeraMaxTldrLen {
		return nil, ErrHeraMessageTldrTooLong
	}
	if strings.ContainsAny(tldr, "\n\r") {
		return nil, ErrHeraMessageTldrMultiline
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Recipient must exist and be active. The FK prevents non-existent IDs but
	// not archived roles (archived_at IS NOT NULL). We must check explicitly.
	var recipientArchivedAt sql.NullString
	err := d.conn.QueryRow(
		`SELECT archived_at FROM hera_roles WHERE id=?`, toRoleID,
	).Scan(&recipientArchivedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && recipientArchivedAt.Valid) {
		return nil, ErrHeraMessageRecipientInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("check recipient role: %w", err)
	}

	// Inbox-full check (per-recipient unread count).
	var unread int
	if err := d.conn.QueryRow(
		`SELECT COUNT(*) FROM hera_messages WHERE to_role_id=? AND read_at IS NULL`, toRoleID,
	).Scan(&unread); err != nil {
		return nil, fmt.Errorf("count hera unread: %w", err)
	}
	if unread >= HeraMaxUnreadPerRole {
		return nil, ErrHeraMessageInboxFull
	}

	// Rate-limit check: rolling 60-second window on the sender.
	since := time.Now().Add(-heraRateLimitWindow)
	var recent int
	if err := d.conn.QueryRow(
		`SELECT COUNT(*) FROM hera_messages WHERE from_role_id=? AND sent_at>=?`,
		fromRoleID, formatTime(since),
	).Scan(&recent); err != nil {
		return nil, fmt.Errorf("count hera recent sends: %w", err)
	}
	if recent >= HeraMaxSendsPerMinute {
		return nil, ErrHeraMessageRateLimited
	}

	// Validate in_reply_to if provided.
	if inReplyTo != nil {
		var exists int
		if err := d.conn.QueryRow(
			`SELECT COUNT(*) FROM hera_messages WHERE id=?`, *inReplyTo,
		).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check in_reply_to: %w", err)
		}
		if exists == 0 {
			return nil, ErrHeraMessageInvalidReplyTo
		}
	}

	now := formatTime(time.Now())
	var replyToArg any
	if inReplyTo != nil {
		replyToArg = *inReplyTo
	}
	res, err := d.conn.Exec(
		`INSERT INTO hera_messages (from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, delivery_mode)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fromRoleID, toRoleID, body, tldr, replyToArg, now, HeraDeliveryPending,
	)
	if err != nil {
		return nil, fmt.Errorf("insert hera message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("insert hera message: last insert id: %w", err)
	}
	return &HeraMessage{
		ID:           id,
		FromRoleID:   fromRoleID,
		ToRoleID:     toRoleID,
		Body:         body,
		Tldr:         tldr,
		InReplyTo:    inReplyTo,
		SentAt:       parseTime(now),
		DeliveryMode: HeraDeliveryPending,
	}, nil
}

// HeraInbox returns unread messages addressed to roleID, oldest first.
func (d *DB) HeraInbox(roleID int64) ([]*HeraMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.conn.Query(
		`SELECT id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, read_at, delivery_mode, delivered_at
		 FROM hera_messages
		 WHERE to_role_id=? AND read_at IS NULL
		 ORDER BY sent_at ASC, id ASC`,
		roleID,
	)
	if err != nil {
		return nil, fmt.Errorf("hera inbox: %w", err)
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

// HeraWorkerMessageSummary reads the latest self-reported line and the exact
// unread count from one worker to its coordinator, without consuming mail.
// A recent-history page cannot count older unread messages correctly.
func (d *DB) HeraWorkerMessageSummary(fromRoleID, toRoleID int64) (string, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var line string
	var unread int
	err := d.conn.QueryRow(`SELECT COALESCE((SELECT tldr FROM hera_messages WHERE from_role_id=? AND to_role_id=? ORDER BY sent_at DESC, id DESC LIMIT 1), ''), (SELECT COUNT(*) FROM hera_messages WHERE from_role_id=? AND to_role_id=? AND read_at IS NULL)`, fromRoleID, toRoleID, fromRoleID, toRoleID).Scan(&line, &unread)
	return line, unread, err
}

// HeraMessagesForTask returns messages addressed to any role taskID is or was
// bound to (ended bindings included), in every read state, oldest first. When
// more than limit match, the newest limit are kept. Read-only: never stamps
// read_at (unlike an agent draining HeraInbox).
func (d *DB) HeraMessagesForTask(taskID string, limit int) ([]*HeraMessage, error) {
	if limit <= 0 {
		limit = HeraMaxUnreadPerRole
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	rows, err := d.conn.Query(
		`SELECT id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, read_at, delivery_mode, delivered_at
		 FROM hera_messages
		 WHERE to_role_id IN (SELECT role_id FROM hera_bindings WHERE argus_task_id=?)
		 ORDER BY sent_at DESC, id DESC LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("hera messages for task: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Reverse(out)
	return out, nil
}

// HeraMessagesPageForTask is the cursor-paged form of HeraMessagesForTask:
// the newest limit messages with id < beforeID (all when beforeID <= 0),
// returned oldest first. more reports whether older matching messages remain.
// Read-only: never stamps read_at or touches delivery state.
func (d *DB) HeraMessagesPageForTask(taskID string, limit int, beforeID int64) (msgs []*HeraMessage, more bool, err error) {
	if limit <= 0 {
		limit = HeraMaxUnreadPerRole
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	query, args := heraMessagesPageQuery(taskID, limit, beforeID)
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("hera message page for task: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanHeraMessage(rows)
		if err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(msgs) > limit {
		more = true
		msgs = msgs[:limit]
	}
	slices.Reverse(msgs)
	return msgs, more, nil
}

// WaitForHeraInbox polls HeraInbox until at least one unread message exists or
// ctx is cancelled. The immediate fast path avoids imposing the polling
// interval when mail arrived before the wait began. HeraInbox intentionally
// uses read_at IS NULL (not the task_messages empty-string convention).
func (d *DB) WaitForHeraInbox(ctx context.Context, roleID int64) ([]*HeraMessage, error) {
	if msgs, err := d.HeraInbox(roleID); err != nil {
		return nil, err
	} else if len(msgs) > 0 {
		return msgs, nil
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			msgs, err := d.HeraInbox(roleID)
			if err != nil {
				return nil, err
			}
			if len(msgs) > 0 {
				return msgs, nil
			}
		}
	}
}

// MarkHeraMessagesRead stamps read_at=now on the given message IDs, but only
// when they are addressed to roleID. IDs addressed to other roles (or that
// don't exist) are silently skipped — keeps the call idempotent for partially-
// overlapping batches. Returns the count of rows actually flipped.
func (d *DB) MarkHeraMessagesRead(roleID int64, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := formatTime(time.Now())
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, roleID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	//nolint:gosec // G202: placeholders are a fixed list of `?` literals; IDs are bound parameters.
	q := `UPDATE hera_messages SET read_at=? WHERE to_role_id=? AND read_at IS NULL AND id IN (` +
		strings.Join(placeholders, ",") + `)`
	res, err := d.conn.Exec(q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark hera messages read: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// HeraMessagesByIDs loads messages by their IDs in an arbitrary order. Missing
// IDs are silently skipped.
func (d *DB) HeraMessagesByIDs(ids []int64) ([]*HeraMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	//nolint:gosec // G202: placeholders are a fixed list of `?` literals; IDs are bound parameters.
	q := `SELECT id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, read_at, delivery_mode, delivered_at
	      FROM hera_messages WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("hera messages by ids: %w", err)
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

// MarkHeraMessageDelivered stamps delivered_at and delivery_mode on a message.
// Idempotent — re-stamping is harmless.
func (d *DB) MarkHeraMessageDelivered(id int64, mode string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec(
		`UPDATE hera_messages SET delivery_mode=?, delivered_at=? WHERE id=?`,
		mode, formatTime(time.Now()), id,
	)
	if err != nil {
		return fmt.Errorf("mark hera message delivered: %w", err)
	}
	return nil
}

// scanHeraMessage reads one HeraMessage from a row using the canonical column
// order: id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at,
// read_at, delivery_mode, delivered_at.
func scanHeraMessage(s rowScanner) (*HeraMessage, error) {
	var m HeraMessage
	var inReplyTo sql.NullInt64
	var sentAt, deliveryMode string
	var readAt, deliveredAt sql.NullString
	if err := s.Scan(
		&m.ID, &m.FromRoleID, &m.ToRoleID, &m.Body, &m.Tldr, &inReplyTo,
		&sentAt, &readAt, &deliveryMode, &deliveredAt,
	); err != nil {
		return nil, fmt.Errorf("scan hera message: %w", err)
	}
	if inReplyTo.Valid {
		v := inReplyTo.Int64
		m.InReplyTo = &v
	}
	m.SentAt = parseTime(sentAt)
	m.ReadAt = nullTimePtr(readAt)
	m.DeliveryMode = deliveryMode
	m.DeliveredAt = nullTimePtr(deliveredAt)
	return &m, nil
}

func heraMessagesPageQuery(taskID string, limit int, beforeID int64) (string, []any) {
	query := `SELECT id, from_role_id, to_role_id, body, tldr, in_reply_to, sent_at, read_at, delivery_mode, delivered_at
 FROM hera_messages
 WHERE to_role_id IN (SELECT role_id FROM hera_bindings WHERE argus_task_id=?)`
	args := []any{taskID}
	if beforeID > 0 {
		query += ` AND id<?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	return query, append(args, limit+1)
}

// sqlExecer is the Exec surface shared by *sql.DB and *sql.Tx.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// isTerminalHeraEndReason reports whether a binding ended because its role can
// never read mail again. Non-terminal ends (hera_rebind, reparented, materialize
// failures) keep mail unread: role-addressed mail waits for a later binding.
func isTerminalHeraEndReason(reason string) bool {
	switch reason {
	case HeraEndReasonUserDeleted, heraEndReasonTaskDeleted, HeraEndReasonTaskMissing, HeraEndReasonMoved:
		return true
	}
	return false
}

// clearDeadRoleMail marks read every unread message addressed to a role with no
// live binding that is nuked or whose most recent binding ended terminally.
// Never-bound roles (planned nodes) are untouched. Idempotent. Best-effort
// callers log and continue on error.
func clearDeadRoleMail(x sqlExecer) (int, error) {
	res, err := x.Exec(`
		UPDATE hera_messages SET read_at=?
		WHERE read_at IS NULL AND to_role_id IN (
			SELECT r.id FROM hera_roles r
			WHERE NOT EXISTS (SELECT 1 FROM hera_bindings b WHERE b.role_id=r.id AND b.ended_at IS NULL)
			  AND ((r.nuked_at IS NOT NULL AND r.nuked_at<>'')
			       OR (SELECT b.end_reason FROM hera_bindings b WHERE b.role_id=r.id ORDER BY b.id DESC LIMIT 1) IN (?,?,?,?))
		)`,
		formatTime(time.Now()),
		HeraEndReasonUserDeleted, heraEndReasonTaskDeleted, HeraEndReasonTaskMissing, HeraEndReasonMoved)
	if err != nil {
		return 0, fmt.Errorf("clear dead-role hera mail: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// clearDeadRoleMailAfterEnd runs the sweep after a binding end when the reason
// is terminal. Best-effort: a failure is logged, never propagated.
func clearDeadRoleMailAfterEnd(x sqlExecer, reason string) {
	if !isTerminalHeraEndReason(reason) {
		return
	}
	if _, err := clearDeadRoleMail(x); err != nil {
		slog.Warn("db: clear hera mail after binding end failed", "reason", reason, "err", err)
	}
}

// ClearDeadRoleMail is the idempotent startup sweep for mail left unread on
// roles that are permanently gone. Returns the number of messages marked read.
func (d *DB) ClearDeadRoleMail() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return clearDeadRoleMail(d.conn)
}
