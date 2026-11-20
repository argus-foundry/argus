package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// Receipts follow the board history lifetime. Role deletion removes both;
// ordinary item edits/resolution never erase the original action or result.
const heraBoardFYISchema = `
CREATE TABLE IF NOT EXISTS hera_board_fyi_receipts (
 role_id INTEGER NOT NULL REFERENCES hera_roles(id) ON DELETE CASCADE,
 action_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 receipt TEXT NOT NULL,
 PRIMARY KEY (role_id,action_id)
);`

type HeraBoardFYIAction struct {
	ActionID          string                `json:"action_id"`
	ItemID            int64                 `json:"item_id"`
	ExpectedUpdatedAt time.Time             `json:"expected_updated_at"`
	Action            string                `json:"action"`
	Note              string                `json:"note"`
	Choice            string                `json:"choice,omitempty"`
	Annotations       []HeraBoardAnnotation `json:"annotations,omitempty"`
}

type HeraBoardFYIResult struct {
	ActionID           string                       `json:"action_id"`
	ItemID             int64                        `json:"item_id"`
	Outcome            string                       `json:"outcome"`
	Item               *HeraBoardItem               `json:"item,omitempty"`
	AnnotationOutcomes []HeraBoardAnnotationOutcome `json:"annotation_outcomes,omitempty"`
}

type HeraBoardFYIActionReceipt struct {
	HeraBoardFYIAction
	RoleID             int64                        `json:"role_id"`
	Fingerprint        string                       `json:"fingerprint"`
	Outcome            string                       `json:"outcome"`
	Item               *HeraBoardItem               `json:"item"`
	CreatedAt          time.Time                    `json:"created_at"`
	AnnotationOutcomes []HeraBoardAnnotationOutcome `json:"annotation_outcomes,omitempty"`
}

type HeraBoardKindCounts struct {
	Decisions int `json:"decisions"`
	FYIs      int `json:"fyis"`
}

// HeraBoardOpenCounts reads all open kind counts in a single SQL snapshot.
// Visibility and rail order belong to the caller; absent roles have zero counts.
func (d *DB) HeraBoardOpenCounts() (map[int64]HeraBoardKindCounts, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT role_id,SUM(kind='decision'),SUM(kind='fyi') FROM hera_board_items WHERE resolved=0 GROUP BY role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int64]HeraBoardKindCounts)
	for rows.Next() {
		var id int64
		var c HeraBoardKindCounts
		if err := rows.Scan(&id, &c.Decisions, &c.FYIs); err != nil {
			return nil, err
		}
		counts[id] = c
	}
	return counts, rows.Err()
}

var boardFYIActionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validFYIEnvelope(actions []HeraBoardFYIAction) bool {
	if len(actions) == 0 || len(actions) > 16 {
		return false
	}
	ids, items := map[string]bool{}, map[int64]bool{}
	for _, a := range actions {
		if !boardFYIActionID.MatchString(a.ActionID) || ids[a.ActionID] || items[a.ItemID] || len(a.Note) > 4096 || !utf8.ValidString(a.Note) || (a.Action == "dismiss" && a.Note != "") {
			return false
		}
		ids[a.ActionID], items[a.ItemID] = true, true
	}
	return true
}

// HeraBoardFYIReceipt returns role-scoped durable history. Authentication is
// enforced by the API caller; decision sessions must not expose this method.
func (d *DB) HeraBoardFYIReceipt(roleID int64, actionID string) (*HeraBoardFYIActionReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return readFYIReceipt(d.conn, roleID, actionID)
}

func readFYIReceipt(q interface{ QueryRow(string, ...any) *sql.Row }, roleID int64, actionID string) (*HeraBoardFYIActionReceipt, error) {
	var raw string
	err := q.QueryRow(`SELECT receipt FROM hera_board_fyi_receipts WHERE role_id=? AND action_id=?`, roleID, actionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHeraBoardNotFound
	}
	if err != nil {
		return nil, err
	}
	var r HeraBoardFYIActionReceipt
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// FYIHeraBoardItems commits successful actions, receipts and at most one
// coordinator summary together. Replay is checked before current item state;
// failed per-action outcomes have no receipt and may be tried again.
func (d *DB) FYIHeraBoardItems(roleID int64, actions []HeraBoardFYIAction) ([]HeraBoardFYIResult, error) {
	if !validFYIEnvelope(actions) {
		uxlog.Log("[board] FYI rejected invalid batch role=%d count=%d", roleID, len(actions))
		return nil, ErrHeraBoardInvalid
	}
	results := make([]HeraBoardFYIResult, 0, len(actions))
	var resolved []int64
	err := d.WithTx(func(tx *sql.Tx) error {
		var comments []*HeraBoardFYIActionReceipt
		for _, a := range actions {
			a.ExpectedUpdatedAt = a.ExpectedUpdatedAt.UTC()
			a.Annotations = append([]HeraBoardAnnotation(nil), a.Annotations...)
			for n := range a.Annotations {
				a.Annotations[n].Quote = ""
				a.Annotations[n].QuoteTruncated = false
			}
			raw, err := json.Marshal(a)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(raw)
			fingerprint := hex.EncodeToString(hash[:])
			r := HeraBoardFYIResult{ActionID: a.ActionID, ItemID: a.ItemID, Outcome: "invalid"}
			previous, err := readFYIReceipt(tx, roleID, a.ActionID)
			if err == nil {
				if previous.Fingerprint == fingerprint {
					r.Outcome, r.Item, r.AnnotationOutcomes = previous.Outcome, previous.Item, previous.AnnotationOutcomes
					uxlog.Log("[board] FYI replay role=%d action=%s item=%d", roleID, a.ActionID, a.ItemID)
				} else {
					uxlog.Log("[board] FYI changed-payload reuse rejected role=%d action=%s", roleID, a.ActionID)
				}
				results = append(results, r)
				continue
			}
			if !errors.Is(err, ErrHeraBoardNotFound) {
				return err
			}
			i, err := scanBoard(tx.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, a.ItemID))
			switch {
			case errors.Is(err, ErrHeraBoardNotFound):
				r.Outcome = "not_found"
			case err != nil:
				return err
			case i.ResidentApproval:
			case i.Kind != "fyi":
				r.Outcome = "not_found"
			case a.ExpectedUpdatedAt.IsZero():
			case a.Action != "dismiss" && a.Action != "comment" && a.Action != "comment_and_dismiss":
			case a.Action != "dismiss" && strings.TrimSpace(a.Note) == "" && len(a.Annotations) == 0:
			case a.Action == "dismiss" && len(a.Annotations) > 0:
			case i.Resolved:
				if a.Action != "comment" && len(a.Annotations) == 0 {
					r.Outcome = "already_resolved"
				}
			case !i.UpdatedAt.Equal(a.ExpectedUpdatedAt):
				r.Outcome, r.Item = "conflict", i
			default:
				var valid bool
				a.Annotations, r.AnnotationOutcomes, valid = validateBoardAnnotations(i, a.Annotations)
				if !valid {
					results = append(results, r)
					continue
				}
				now := time.Now().UTC()
				r.Outcome = "commented"
				if a.Action != "comment" {
					// Always advance the revision, including under a coarse/frozen clock.
					if !now.After(i.UpdatedAt) {
						now = i.UpdatedAt.Add(time.Nanosecond)
					}
					_, err = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by='human',resolution_choice='',resolution_note=?,updated_at=? WHERE role_id=? AND id=?`, a.Note, formatTime(now), roleID, a.ItemID)
					if err != nil {
						return err
					}
					i.Resolved, i.ResolvedBy, i.ResolutionChoice, i.ResolutionNote, i.UpdatedAt = true, "human", "", a.Note, now
					r.Outcome = "resolved"
					resolved = append(resolved, i.ID)
				}
				r.Item = i
				receipt := &HeraBoardFYIActionReceipt{HeraBoardFYIAction: a, RoleID: roleID, Fingerprint: fingerprint, Outcome: r.Outcome, Item: i, CreatedAt: now, AnnotationOutcomes: r.AnnotationOutcomes}
				raw, err = json.Marshal(receipt)
				if err != nil {
					return err
				}
				_, err = tx.Exec(`INSERT INTO hera_board_fyi_receipts(role_id,action_id,fingerprint,receipt) VALUES(?,?,?,?)`, roleID, a.ActionID, fingerprint, string(raw))
				if err != nil {
					return err
				}
				if a.Action != "dismiss" {
					comments = append(comments, receipt)
				}
			}
			results = append(results, r)
		}
		if len(comments) > 0 {
			return writeFYISummary(tx, roleID, comments)
		}
		return nil
	})
	if err != nil {
		uxlog.Log("[board] FYI batch failed role=%d: %v", roleID, err)
		return nil, err
	}
	for _, r := range results {
		uxlog.Log("[board] FYI role=%d action=%s item=%d outcome=%s", roleID, r.ActionID, r.ItemID, r.Outcome)
	}
	for _, id := range resolved {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	return results, nil
}

func writeFYISummary(tx *sql.Tx, roleID int64, comments []*HeraBoardFYIActionReceipt) error {
	sort.Slice(comments, func(i, j int) bool { return comments[i].ItemID < comments[j].ItemID })
	rows, err := tx.Query(`SELECT id,kind FROM hera_board_items WHERE role_id=? AND resolved=0 ORDER BY id`, roleID)
	if err != nil {
		return err
	}
	var decisions, fyis []string
	for rows.Next() {
		var id int64
		var kind string
		if err = rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return err
		}
		if kind == "decision" {
			decisions = append(decisions, fmt.Sprintf("#%d", id))
		} else {
			fyis = append(fyis, fmt.Sprintf("#%d", id))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tldr := fmt.Sprintf("Aaron (board) commented on %d FYIs", len(comments))
	head := tldr + ". Feedback from Aaron (board):\n\n"
	tail := "\n\nStill open decisions: " + strings.Join(decisions, ", ") + "\nStill open FYIs: " + strings.Join(fyis, ", ")
	sections := make([]string, len(comments))
	for n, c := range comments {
		state := "open; comment only"
		if c.Action == "comment_and_dismiss" {
			state = "already resolved; comment and dismiss"
		}
		sections[n] = fmt.Sprintf("#%d %s\n  State: %s\n  Note: ", c.ItemID, c.Item.Subject, state)
	}
	budget := (64*1024 - len(head) - len(tail) - len(strings.Join(sections, "\n\n"))) / len(comments)
	if budget < 0 {
		return ErrHeraBoardInvalid
	}
	for n, c := range comments {
		note := c.Note
		if len(c.Annotations) > 0 {
			note, err = boardFeedback(c.Note, c.Annotations, budget, roleID, c.ActionID)
			if err != nil {
				return err
			}
			sections[n] += note
			continue
		}
		if len(note) > budget {
			marker := fmt.Sprintf("… [truncated — full comment: authenticated GET /api/hera/roles/%d/board/fyi-actions/%s; role %d action %s]", roleID, c.ActionID, roleID, c.ActionID)
			limit := budget - len(marker)
			if limit < 0 {
				return ErrHeraBoardInvalid
			}
			for limit > 0 && !utf8.ValidString(note[:limit]) {
				limit--
			}
			note = note[:limit] + marker
		}
		sections[n] += note
	}
	_, err = tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,?)`, roleID, roleID, head+strings.Join(sections, "\n\n")+tail, tldr, formatTime(time.Now()), HeraDeliveryPending)
	return err
}
