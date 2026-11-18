package db

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ApprovalCall is the complete identity of a deferred attempt. Launch is a
// public fingerprint of the resident's per-launch private hook credential.
type ApprovalCall struct {
	TaskID, Launch, Session, Cwd, Tool string
	Input                              json.RawMessage
	Server                             json.RawMessage
}

type ResidentApproval struct {
	BoardID, RoleID int64
	CreatedAt       time.Time
	State           string
	// Reused reports that an identical request was already pending and no
	// new item was created; Superseded lists older pending items this new
	// request replaced.
	Reused     bool
	Superseded []int64
}

var ErrResidentApproval = errors.New("resident approval: invalid, stale or already decided request")
var approvalToolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,240}$`)

// canonicalApprovalJSON rejects duplicate object keys and trailing values;
// UseNumber keeps connector IDs above 2^53 exact. Lexically different numbers
// remain different, a conservative mismatch rather than a widened grant.
func canonicalApprovalJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 48*1024 || !utf8.Valid(raw) {
		return nil, ErrResidentApproval
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, err := approvalJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrResidentApproval
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrResidentApproval
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > 48*1024 {
		return nil, ErrResidentApproval
	}
	return canonical, nil
}

func approvalJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, ErrResidentApproval
	}
	tok, err := d.Token()
	if err != nil {
		return nil, ErrResidentApproval
	}
	switch tok {
	case json.Delim('{'):
		out := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, ErrResidentApproval
			}
			s, ok := key.(string)
			if !ok {
				return nil, ErrResidentApproval
			}
			if _, exists := out[s]; exists {
				return nil, ErrResidentApproval
			}
			out[s], err = approvalJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		_, err = d.Token()
		return out, err
	case json.Delim('['):
		out := []any{}
		for d.More() {
			v, err := approvalJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		_, err = d.Token()
		return out, err
	default:
		return tok, nil
	}
}


// ResidentApprovalWindow is how long an approved grant stays usable after the
// human approves it. A pending request never times out; it lapses only when
// the resident is relaunched (fix-resident-approval-flow).
const ResidentApprovalWindow = 60 * time.Minute

// Approval states. Only ApprovalApproved authorizes, and only on its launch
// before expires_at.
const (
	ApprovalPending    = "pending"
	ApprovalApproved   = "approved"
	ApprovalConsumed   = "consumed"
	ApprovalDenied     = "denied"
	ApprovalStale      = "stale"
	ApprovalExpired    = "expired"
	ApprovalWithdrawn  = "withdrawn"
	ApprovalSuperseded = "superseded"
)

// approvalResolver is the resolved_by value for transitions Argus makes on
// its own (lapse, expiry, supersede).
const approvalResolver = "argus"

// ApprovalStateError explains why an approval item cannot be decided or
// withdrawn in its current state. It matches ErrResidentApproval so callers
// testing for the generic error keep working.
type ApprovalStateError struct {
	ID    int64
	State string
	Msg   string
}

func (e *ApprovalStateError) Error() string { return fmt.Sprintf("deferred call #%d: %s", e.ID, e.Msg) }

func (e *ApprovalStateError) Is(target error) bool { return target == ErrResidentApproval }

// approvalRow is one resident_approvals row plus its board subject.
type approvalRow struct {
	id, roleID, supersededBy                int64
	launch, state                           string
	created, decided, closed, expires, body string
}

const approvalRowColumns = `a.board_id,a.role_id,a.superseded_by,a.launch,a.state,a.created_at,a.decided_at,a.closed_at,a.expires_at,b.body`

func scanApprovalRow(s rowScanner) (approvalRow, error) {
	var r approvalRow
	err := s.Scan(&r.id, &r.roleID, &r.supersededBy, &r.launch, &r.state, &r.created, &r.decided, &r.closed, &r.expires, &r.body)
	return r, err
}

func loadApprovalTx(tx *sql.Tx, id int64) (approvalRow, error) {
	r, err := scanApprovalRow(tx.QueryRow(`SELECT `+approvalRowColumns+` FROM resident_approvals a JOIN hera_board_items b ON b.id=a.board_id WHERE a.board_id=?`, id))
	if err != nil {
		return r, ErrResidentApproval
	}
	return r, nil
}

// approvalClock renders a stored time for a human: 15:04 today, otherwise
// with the date.
func approvalClock(stored string, now time.Time) string {
	t := parseTime(stored)
	if t.IsZero() {
		return "an unknown time"
	}
	t, n := t.Local(), now.Local()
	if t.Year() == n.Year() && t.YearDay() == n.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// describeApproval explains a non-pending row's state for a human or agent.
func describeApproval(r approvalRow, now time.Time) string {
	decided, closed := approvalClock(r.decided, now), approvalClock(r.closed, now)
	switch r.state {
	case ApprovalApproved:
		if !now.Before(parseTime(r.expires)) {
			return fmt.Sprintf("approved at %s, but the approval expired unused at %s. Ask the agent to re-send the call.", decided, approvalClock(r.expires, now))
		}
		return fmt.Sprintf("already approved at %s; waiting for the agent to run it (good until %s).", decided, approvalClock(r.expires, now))
	case ApprovalConsumed:
		return fmt.Sprintf("already approved at %s and used: the agent ran it at %s.", decided, closed)
	case ApprovalDenied:
		return fmt.Sprintf("already denied at %s.", decided)
	case ApprovalStale:
		return fmt.Sprintf("lapsed at %s because the agent was relaunched before using it. Ask the agent to re-send the call.", closed)
	case ApprovalExpired:
		return fmt.Sprintf("approved at %s, but the approval expired unused at %s. Ask the agent to re-send the call.", decided, closed)
	case ApprovalWithdrawn:
		return fmt.Sprintf("withdrawn by the agent at %s.", closed)
	case ApprovalSuperseded:
		return fmt.Sprintf("superseded by #%d at %s; decide #%d instead.", r.supersededBy, closed, r.supersededBy)
	}
	return "is in an unknown state (" + r.state + ")."
}

func approvalStateErr(r approvalRow, now time.Time) error {
	return &ApprovalStateError{ID: r.id, State: r.state, Msg: describeApproval(r, now)}
}

// closeApprovalBoardTx resolves (or re-notes) the approval's board item.
func closeApprovalBoardTx(tx *sql.Tx, id int64, by, note string, now time.Time) error {
	_, err := tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by=?,resolution_note=?,updated_at=? WHERE id=?`, by, note, formatTime(now), id)
	return err
}

func insertApprovalNoticeTx(tx *sql.Tx, roleID int64, body, tldr string, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,?)`, roleID, roleID, body, tldr, formatTime(now), HeraDeliveryPending)
	return err
}

func approvalBoardBody(call ApprovalCall, input []byte) string {
	return fmt.Sprintf("Tool: %s\n\nWorking directory: %s\n\nExact input:\n%s\n\nServer:\n%s\n\nReply approves once; dismiss denies. This request does not time out. After approval the agent has %d minutes to retry this exact call once. If the agent is relaunched before then, this item lapses and the agent is told to re-send.", call.Tool, call.Cwd, input, call.Server, int(ResidentApprovalWindow/time.Minute))
}

// RequestResidentApproval either consumes one exact grant or records a durable
// decision. The write lock precedes reads, including across DB handles.
// Queuing a new request supersedes older pending requests for the same call
// content on any launch or session.
func (d *DB) RequestResidentApproval(call ApprovalCall, now time.Time) (*ResidentApproval, bool, error) {
	if call.TaskID == "" || call.Launch == "" || call.Session == "" || call.Cwd == "" || !approvalToolName.MatchString(call.Tool) {
		return nil, false, ErrResidentApproval
	}
	if len(call.Input)+len(call.Server) > 48*1024 {
		return nil, false, ErrResidentApproval
	}
	input, err := canonicalApprovalJSON(call.Input)
	if err != nil {
		return nil, false, err
	}
	call.Input = input
	if call.Tool == "Bash" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(input, &fields); err != nil {
			return nil, false, err
		}
		delete(fields, "description")
		call.Input, err = json.Marshal(fields)
		if err != nil {
			return nil, false, err
		}
		input = call.Input
	}
	if len(call.Server) > 0 {
		call.Server, err = canonicalApprovalJSON(call.Server)
		if err != nil {
			return nil, false, err
		}
	}
	if len(call.Input)+len(call.Server) > 48*1024 {
		return nil, false, ErrResidentApproval
	}
	identity, err := json.Marshal(call)
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(identity)
	key := hex.EncodeToString(sum[:])
	content := call
	content.Launch, content.Session = "", ""
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, false, err
	}
	csum := sha256.Sum256(contentJSON)
	callKey := hex.EncodeToString(csum[:])
	var a ResidentApproval
	allow := false
	var changed []int64
	err = d.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE resident_approvals SET state=state WHERE identity=?`, key); err != nil {
			return err
		}
		var roleID int64
		if err := tx.QueryRow(`SELECT b.role_id FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id JOIN hera_roles r ON r.id=b.role_id WHERE t.id=? AND t.kind=? AND b.ended_at IS NULL AND r.kind='coordinator'`, call.TaskID, model.KindResident).Scan(&roleID); err != nil {
			return ErrResidentApproval
		}
		r, err := scanApprovalRow(tx.QueryRow(`SELECT `+approvalRowColumns+` FROM resident_approvals a JOIN hera_board_items b ON b.id=a.board_id WHERE a.identity=? ORDER BY a.board_id DESC LIMIT 1`, key))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			a = ResidentApproval{BoardID: r.id, RoleID: r.roleID, CreatedAt: parseTime(r.created), State: r.state}
			switch r.state {
			case ApprovalPending:
				a.Reused = true
				return nil
			case ApprovalDenied:
				return nil
			case ApprovalApproved:
				if now.Before(parseTime(r.expires)) {
					result, err := tx.Exec(`UPDATE resident_approvals SET state=?,closed_at=? WHERE board_id=? AND state=?`, ApprovalConsumed, formatTime(now), r.id, ApprovalApproved)
					if err != nil {
						return err
					}
					rows, err := result.RowsAffected()
					if err != nil || rows != 1 {
						return ErrResidentApproval
					}
					note := fmt.Sprintf("approved %s; ran %s", approvalClock(r.decided, now), approvalClock(formatTime(now), now))
					if err := closeApprovalBoardTx(tx, r.id, "human", note, now); err != nil {
						return err
					}
					allow = true
					a.State = ApprovalConsumed
					changed = append(changed, r.id)
					return nil
				}
				// The agent is retrying right now, so its reply explains the
				// new request; no separate expiry notice.
				if _, err := tx.Exec(`UPDATE resident_approvals SET state=?,closed_at=? WHERE board_id=?`, ApprovalExpired, formatTime(now), r.id); err != nil {
					return err
				}
				note := fmt.Sprintf("approved %s; expired unused %s", approvalClock(r.decided, now), approvalClock(r.expires, now))
				if err := closeApprovalBoardTx(tx, r.id, approvalResolver, note, now); err != nil {
					return err
				}
				changed = append(changed, r.id)
			}
		}
		created := formatTime(now)
		subject := "Approve call: " + call.Tool
		body := approvalBoardBody(call, input)
		if !validBoardContent("decision", subject, body) {
			return ErrResidentApproval
		}
		res, err := tx.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,created_at,updated_at) VALUES (?,'decision',?,?,?,?)`, roleID, subject, body, created, created)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO resident_approvals(board_id,role_id,identity,call_key,launch,created_at,state,expires_at) VALUES(?,?,?,?,?,?,?,'')`, id, roleID, key, callKey, call.Launch, created, ApprovalPending); err != nil {
			return err
		}
		a = ResidentApproval{BoardID: id, RoleID: roleID, CreatedAt: parseTime(created), State: ApprovalPending}
		rows, err := tx.Query(`SELECT board_id FROM resident_approvals WHERE call_key=? AND role_id=? AND state=? AND board_id<>? ORDER BY board_id`, callKey, roleID, ApprovalPending, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var old int64
			if err := rows.Scan(&old); err != nil {
				rows.Close() //nolint:errcheck
				return err
			}
			a.Superseded = append(a.Superseded, old)
		}
		rows.Close() //nolint:errcheck
		if err := rows.Err(); err != nil {
			return err
		}
		for _, old := range a.Superseded {
			if _, err := tx.Exec(`UPDATE resident_approvals SET state=?,superseded_by=?,closed_at=? WHERE board_id=?`, ApprovalSuperseded, id, created, old); err != nil {
				return err
			}
			if err := closeApprovalBoardTx(tx, old, approvalResolver, fmt.Sprintf("superseded by #%d", id), now); err != nil {
				return err
			}
			changed = append(changed, old)
		}
		return nil
	})
	if err != nil {
		uxlog.Log("[resident-approval] request failed: %v", err)
		return nil, false, err
	}
	uxlog.Log("[resident-approval] request item=%d state=%s allow=%t reused=%t superseded=%v", a.BoardID, a.State, allow, a.Reused, a.Superseded)
	for _, id := range changed {
		if id != a.BoardID {
			d.emitBoardChanged(a.RoleID, id, "resolved")
		}
	}
	d.emitBoardChanged(a.RoleID, a.BoardID, "updated")
	return &a, allow, nil
}

// DecideResidentApproval is deliberately absent from MCP/REST/RPC. The local
// human TUI calls it after confirmation; ordinary board resolution cannot grant.
// A non-pending item returns an *ApprovalStateError naming its real state.
func (d *DB) DecideResidentApproval(id int64, expected time.Time, launch string, approve bool, now time.Time) error {
	var roleID int64
	err := d.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE resident_approvals SET state=state WHERE board_id=?`, id); err != nil {
			return err
		}
		r, err := loadApprovalTx(tx, id)
		if err != nil {
			return err
		}
		roleID = r.roleID
		if r.state != ApprovalPending {
			return approvalStateErr(r, now)
		}
		if expected.IsZero() || r.created != formatTime(expected) {
			return &ApprovalStateError{ID: id, State: r.state, Msg: "changed since you opened it; reopen it and review again."}
		}
		if approve && (launch == "" || r.launch != launch) {
			return &ApprovalStateError{ID: id, State: ApprovalStale, Msg: "lapsed because the agent was relaunched after it was queued. Ask the agent to re-send the call."}
		}
		state, note, expires := ApprovalDenied, "denied "+approvalClock(formatTime(now), now), ""
		if approve {
			state = ApprovalApproved
			expires = formatTime(now.Add(ResidentApprovalWindow))
			note = fmt.Sprintf("approved %s; good until %s", approvalClock(formatTime(now), now), approvalClock(expires, now))
		}
		if _, err := tx.Exec(`UPDATE resident_approvals SET state=?,decided_at=?,expires_at=? WHERE board_id=?`, state, formatTime(now), expires, id); err != nil {
			return err
		}
		if err := closeApprovalBoardTx(tx, id, "human", note, now); err != nil {
			return err
		}
		body := fmt.Sprintf("[argus] Aaron %s deferred call #%d. ", state, id)
		if approve {
			body += fmt.Sprintf("Retry the exact original tool and input once within %d minutes (until %s) if it is still appropriate; nothing has run yet. If you are relaunched first, the approval lapses and you will be told to re-send.\n\nStored canonical call:\n%s", int(ResidentApprovalWindow/time.Minute), approvalClock(expires, now), r.body)
		} else {
			body += "Do not retry this call; continue other work."
		}
		return insertApprovalNoticeTx(tx, roleID, body, fmt.Sprintf("Deferred call #%d %s", id, state), now)
	})
	uxlog.Log("[resident-approval] decision item=%d approve=%t err=%v", id, approve, err)
	if err == nil {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	return err
}

// SweepResidentApprovals closes approval items that can no longer act: a
// pending or approved item from a launch other than currentLaunch becomes
// stale, and an approved item past its window becomes expired. Each lapse
// resolves its board item and queues one [argus] notice in the same
// transaction, so it is announced exactly once. An empty currentLaunch (no
// readable launch file) skips the launch comparison.
func (d *DB) SweepResidentApprovals(currentLaunch string, now time.Time) (int, error) {
	type lapse struct{ id, roleID int64 }
	var lapsed []lapse
	err := d.WithTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT `+approvalRowColumns+` FROM resident_approvals a JOIN hera_board_items b ON b.id=a.board_id WHERE a.state IN (?,?) ORDER BY a.board_id`, ApprovalPending, ApprovalApproved)
		if err != nil {
			return err
		}
		var open []approvalRow
		for rows.Next() {
			r, err := scanApprovalRow(rows)
			if err != nil {
				rows.Close() //nolint:errcheck
				return err
			}
			open = append(open, r)
		}
		rows.Close() //nolint:errcheck
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range open {
			var state, note, why string
			switch {
			case currentLaunch != "" && r.launch != currentLaunch:
				state = ApprovalStale
				note = "lapsed " + approvalClock(formatTime(now), now) + ": relaunched before use"
				if r.state == ApprovalApproved {
					note = fmt.Sprintf("approved %s; %s", approvalClock(r.decided, now), note)
				}
				why = "lapsed because you were relaunched before it was used"
			case r.state == ApprovalApproved && !now.Before(parseTime(r.expires)):
				state = ApprovalExpired
				note = fmt.Sprintf("approved %s; expired unused %s", approvalClock(r.decided, now), approvalClock(r.expires, now))
				why = fmt.Sprintf("was approved, but the %d-minute retry window passed unused", int(ResidentApprovalWindow/time.Minute))
			default:
				continue
			}
			res, err := tx.Exec(`UPDATE resident_approvals SET state=?,closed_at=? WHERE board_id=? AND state=?`, state, formatTime(now), r.id, r.state)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				continue
			}
			if err := closeApprovalBoardTx(tx, r.id, approvalResolver, note, now); err != nil {
				return err
			}
			body := fmt.Sprintf("[argus] Deferred call #%d %s, so nothing ran. If it is still needed, re-send the exact call; it will queue a fresh approval on Aaron's board.\n\nStored canonical call:\n%s", r.id, why, r.body)
			if err := insertApprovalNoticeTx(tx, r.roleID, body, fmt.Sprintf("Deferred call #%d %s", r.id, state), now); err != nil {
				return err
			}
			lapsed = append(lapsed, lapse{r.id, r.roleID})
		}
		return nil
	})
	if err != nil {
		uxlog.Log("[resident-approval] sweep failed: %v", err)
		return 0, err
	}
	for _, l := range lapsed {
		uxlog.Log("[resident-approval] lapsed item=%d", l.id)
		d.emitBoardChanged(l.roleID, l.id, "resolved")
	}
	return len(lapsed), nil
}

// withdrawResidentApprovalTx is the agent's own cancellation of a pending
// approval item. It can only close an item, never grant one; any other state
// returns the state-specific error.
func withdrawResidentApprovalTx(tx *sql.Tx, id int64, note string, now time.Time) error {
	r, err := loadApprovalTx(tx, id)
	if err != nil {
		return err
	}
	if r.state != ApprovalPending {
		return approvalStateErr(r, now)
	}
	if _, err := tx.Exec(`UPDATE resident_approvals SET state=?,closed_at=? WHERE board_id=? AND state=?`, ApprovalWithdrawn, formatTime(now), id, ApprovalPending); err != nil {
		return err
	}
	text := "withdrawn by agent"
	if note != "" {
		text += ": " + note
	}
	return closeApprovalBoardTx(tx, id, "agent", text, now)
}

// residentApprovalStateErrTx explains, for a generic human dismissal, why an
// approval item must be decided with approve or deny instead.
func residentApprovalStateErrTx(tx *sql.Tx, id int64, now time.Time) error {
	r, err := loadApprovalTx(tx, id)
	if err != nil {
		return err
	}
	if r.state == ApprovalPending {
		return &ApprovalStateError{ID: id, State: r.state, Msg: "is waiting for approval; use approve or deny instead of dismissing it."}
	}
	return approvalStateErr(r, now)
}
