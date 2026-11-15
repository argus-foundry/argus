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
}

var ErrResidentApproval = errors.New("resident approval: invalid, stale or already decided request")
var approvalToolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,240}$`)

// canonicalApprovalJSON rejects duplicate object keys and trailing values;
// UseNumber keeps connector IDs above 2^53 exact. Lexically different numbers
// remain different, a conservative mismatch rather than a widened grant.
func canonicalApprovalJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 48*1024 {
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
	return json.Marshal(value)
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

// RequestResidentApproval either consumes one exact grant or records a durable
// decision. The write lock precedes reads, including across DB handles.
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
	if len(call.Server) > 0 {
		call.Server, err = canonicalApprovalJSON(call.Server)
		if err != nil {
			return nil, false, err
		}
	}
	identity, err := json.Marshal(call)
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(identity)
	key := hex.EncodeToString(sum[:])
	var a ResidentApproval
	allow := false
	err = d.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE resident_approvals SET state=state WHERE identity=?`, key); err != nil {
			return err
		}
		var roleID int64
		if err := tx.QueryRow(`SELECT b.role_id FROM hera_bindings b JOIN tasks t ON t.id=b.argus_task_id JOIN hera_roles r ON r.id=b.role_id WHERE t.id=? AND t.kind=? AND b.ended_at IS NULL AND r.kind='coordinator'`, call.TaskID, model.KindResident).Scan(&roleID); err != nil {
			return ErrResidentApproval
		}
		var created, expires string
		err := tx.QueryRow(`SELECT board_id,role_id,created_at,state,expires_at FROM resident_approvals WHERE identity=? ORDER BY board_id DESC LIMIT 1`, key).Scan(&a.BoardID, &a.RoleID, &created, &a.State, &expires)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			a.CreatedAt = parseTime(created)
			switch a.State {
			case "pending", "denied":
				return nil
			case "approved":
				if now.Before(parseTime(expires)) {
					_, err = tx.Exec(`UPDATE resident_approvals SET state='consumed' WHERE board_id=? AND state='approved'`, a.BoardID)
					allow = err == nil
					a.State = "consumed"
					return err
				}
			}
		}
		created = formatTime(now)
		body := fmt.Sprintf("Tool: %s\n\nWorking directory: %s\n\nExact input:\n%s\n\nServer:\n%s\n\nReply opens Approve once; dismiss opens Deny. Approval expires after ten minutes and applies only to this launch.", call.Tool, call.Cwd, input, call.Server)
		res, err := tx.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,created_at,updated_at) VALUES (?,'decision',?,?,?,?)`, roleID, "Approve Solo call: "+call.Tool, body, created, created)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO resident_approvals(board_id,role_id,identity,launch,created_at,state,expires_at) VALUES(?,?,?,?,?,'pending','')`, id, roleID, key, call.Launch, created)
		a = ResidentApproval{BoardID: id, RoleID: roleID, CreatedAt: parseTime(created), State: "pending"}
		return err
	})
	if err != nil {
		uxlog.Log("[resident-approval] request failed: %v", err)
		return nil, false, err
	}
	uxlog.Log("[resident-approval] request item=%d state=%s allow=%t", a.BoardID, a.State, allow)
	d.emitBoardChanged(a.RoleID, a.BoardID, "updated")
	return &a, allow, nil
}

// DecideResidentApproval is deliberately absent from MCP/REST/RPC. The local
// human TUI calls it after confirmation; ordinary board resolution cannot grant.
func (d *DB) DecideResidentApproval(id int64, expected time.Time, launch string, approve bool, now time.Time) error {
	var roleID int64
	err := d.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE resident_approvals SET state=state WHERE board_id=?`, id); err != nil {
			return err
		}
		var created, currentLaunch, state string
		if err := tx.QueryRow(`SELECT role_id,created_at,launch,state FROM resident_approvals WHERE board_id=?`, id).Scan(&roleID, &created, &currentLaunch, &state); err != nil {
			return ErrResidentApproval
		}
		if state != "pending" || expected.IsZero() || created != formatTime(expected) || (approve && (launch == "" || currentLaunch != launch)) {
			return ErrResidentApproval
		}
		state = "denied"
		if approve {
			state = "approved"
		}
		_, err := tx.Exec(`UPDATE resident_approvals SET state=?,expires_at=? WHERE board_id=?`, state, formatTime(now.Add(10*time.Minute)), id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by='human',resolution_note=?,updated_at=? WHERE id=?`, state, formatTime(now), id)
		if err != nil {
			return err
		}
		body := fmt.Sprintf("[argus] Aaron %s deferred call #%d. ", state, id)
		if approve {
			body += "Retry the exact original tool and input once within ten minutes if it is still appropriate. A changed launch or policy requires a new approval."
		} else {
			body += "Do not retry this call; continue other work."
		}
		_, err = tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,'pending')`, roleID, roleID, body, fmt.Sprintf("Deferred call #%d %s", id, state), formatTime(now))
		return err
	})
	uxlog.Log("[resident-approval] decision item=%d approve=%t err=%v", id, approve, err)
	if err == nil {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	return err
}
