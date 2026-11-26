package db

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/uxlog"
)

type HeraBoardOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Summary  string `json:"summary"`
	Upside   string `json:"upside"`
	Downside string `json:"downside"`
}

type HeraBoardStructure struct {
	Options     []HeraBoardOption `json:"options"`
	Recommended string            `json:"recommended"`
	Reason      string            `json:"reason"`
}

func firstBoardStructure(s []*HeraBoardStructure) *HeraBoardStructure {
	if len(s) == 0 {
		return nil
	}
	return s[0]
}

var boardOptionID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func encodeBoardStructure(kind string, s *HeraBoardStructure) (string, error) {
	if s == nil {
		return "", nil
	}
	if kind != "decision" || len(s.Options) < 2 || len(s.Options) > 4 {
		return "", ErrHeraBoardInvalid
	}
	validText := func(v string, cap int, newline bool) bool {
		return utf8.ValidString(v) && utf8.RuneCountInString(v) <= cap && !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) && (!newline || r != '\n') })
	}
	seen := map[string]bool{}
	for _, o := range s.Options {
		if !boardOptionID.MatchString(o.ID) || o.ID == "other" || seen[o.ID] || strings.TrimSpace(o.Label) == "" || !validText(o.Label, 80, false) || !validText(o.Summary, 400, false) || !validText(o.Upside, 400, false) || !validText(o.Downside, 400, false) {
			return "", ErrHeraBoardInvalid
		}
		seen[o.ID] = true
	}
	if (s.Recommended != "" && !seen[s.Recommended]) || !validText(s.Reason, 400, true) {
		return "", ErrHeraBoardInvalid
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(raw) > 16*1024 {
		return "", ErrHeraBoardInvalid
	}
	return string(raw), nil
}

type HeraBoardAnswer struct {
	ActionID          string                `json:"action_id,omitempty"`
	Action            string                `json:"action,omitempty"`
	Annotations       []HeraBoardAnnotation `json:"annotations,omitempty"`
	ItemID            int64                 `json:"item_id"`
	ExpectedUpdatedAt time.Time             `json:"expected_updated_at"`
	Choice            string                `json:"choice"`
	Note              string                `json:"note"`
}

type HeraBoardAnswerResult struct {
	ActionID           string                       `json:"action_id,omitempty"`
	ItemID             int64                        `json:"item_id"`
	Outcome            string                       `json:"outcome"`
	Item               *HeraBoardItem               `json:"item,omitempty"`
	AnnotationOutcomes []HeraBoardAnnotationOutcome `json:"annotation_outcomes,omitempty"`
}

// AnswerHeraBoardItems commits the successful answers and their single notice
// atomically. Invalid/stale answers do not prevent other answers in the round.
func (d *DB) AnswerHeraBoardItems(roleID int64, answers []HeraBoardAnswer) ([]HeraBoardAnswerResult, error) {
	if len(answers) == 0 || len(answers) > 16 {
		return nil, ErrHeraBoardInvalid
	}
	results, err := d.answerHeraBoardItems(roleID, answers)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		uxlog.Log("[board] answer role=%d item=%d outcome=%s", roleID, r.ItemID, r.Outcome)
	}
	return results, nil
}

func (d *DB) answerHeraBoardItems(roleID int64, answers []HeraBoardAnswer) ([]HeraBoardAnswerResult, error) {
	d.mu.Lock()
	var newlyResolved []int64
	committed := false
	defer func() {
		d.mu.Unlock()
		if committed {
			for _, id := range newlyResolved {
				d.emitBoardChanged(roleID, id, "resolved")
			}
		}
	}()
	tx, err := d.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	answers = append([]HeraBoardAnswer(nil), answers...)
	sort.SliceStable(answers, func(i, j int) bool { return answers[i].ItemID < answers[j].ItemID })
	results := make([]HeraBoardAnswerResult, 0, len(answers))
	var sections []string
	var notes []string
	var answeredIDs []int64
	var receiptIDs []string
	var feedbackAnnotations [][]HeraBoardAnnotation
	answeredCount, commentCount := 0, 0
	now := formatTime(time.Now())
	for _, a := range answers {
		r := HeraBoardAnswerResult{ActionID: a.ActionID, ItemID: a.ItemID, Outcome: "invalid"}
		durable := a.ActionID != "" || a.Action == "comment" || len(a.Annotations) > 0
		if a.Action == "" {
			a.Action = "answer"
		}
		a.ExpectedUpdatedAt = a.ExpectedUpdatedAt.UTC()
		a.Annotations = append([]HeraBoardAnnotation(nil), a.Annotations...)
		// Fingerprint only client fields; derived quote must never affect replay.
		for n := range a.Annotations {
			a.Annotations[n].Quote = ""
			a.Annotations[n].QuoteTruncated = false
		}
		raw, e := json.Marshal(a)
		if e != nil {
			return nil, e
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
		if durable {
			if !boardFYIActionID.MatchString(a.ActionID) {
				results = append(results, r)
				continue
			}
			previous, e := readFYIReceipt(tx, roleID, a.ActionID)
			if e == nil {
				if previous.Fingerprint == fingerprint {
					r.Outcome, r.Item, r.AnnotationOutcomes = previous.Outcome, previous.Item, previous.AnnotationOutcomes
				}
				results = append(results, r)
				continue
			}
			if !errors.Is(e, ErrHeraBoardNotFound) {
				return nil, e
			}
		}
		i, e := scanBoard(tx.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, a.ItemID))
		switch {
		case errors.Is(e, ErrHeraBoardNotFound):
			r.Outcome = "not_found"
		case e != nil:
			return nil, e
		case i.Kind != "decision":
			r.Outcome = "not_found"
		case i.ResidentApproval:
		case i.Resolved:
			if !durable {
				r.Outcome = "already_resolved"
			} else {
				r.Item = i
				uxlog.Log("[board] answer closed feedback rejected role=%d item=%d", roleID, a.ItemID)
			}
		case a.ExpectedUpdatedAt.IsZero() || len(a.Note) > 4096 || !utf8.ValidString(a.Note):
		case a.Action != "answer" && a.Action != "comment":
		case !i.UpdatedAt.Equal(a.ExpectedUpdatedAt):
			r.Outcome = "conflict"
			r.Item = i
		default:
			var valid bool
			a.Annotations, r.AnnotationOutcomes, valid = validateBoardAnnotations(i, a.Annotations)
			if !valid {
				results = append(results, r)
				continue
			}
			label := ""
			if a.Choice == "other" && strings.TrimSpace(a.Note) != "" {
				label = "Something else"
			}
			if i.Structured != nil {
				for _, o := range i.Structured.Options {
					if a.Choice == o.ID {
						label = o.Label
						if a.Choice == i.Structured.Recommended {
							label += " (the recommendation)"
						}
					}
				}
			}
			if a.Action == "comment" {
				if a.Choice != "" || (strings.TrimSpace(a.Note) == "" && len(a.Annotations) == 0) {
					results = append(results, r)
					continue
				}
				r.Outcome, r.Item = "commented", i
				commentCount++
				sections = append(sections, fmt.Sprintf("#%d %s\n  State: open; comment only\n  Note: ", i.ID, i.Subject))
			} else if label != "" {
				_, e = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by='human',resolution_choice=?,resolution_note=?,updated_at=? WHERE role_id=? AND id=?`, a.Choice, a.Note, now, roleID, a.ItemID)
				if e != nil {
					return nil, e
				}
				r.Outcome = "resolved"
				i.Resolved, i.ResolvedBy, i.ResolutionChoice, i.ResolutionNote = true, "human", a.Choice, a.Note
				i.UpdatedAt, _ = time.Parse(time.RFC3339Nano, now)
				if durable {
					r.Item = i
				}
				newlyResolved = append(newlyResolved, i.ID)
				answeredCount++
				sections = append(sections, fmt.Sprintf("#%d %s\n  Choice: %s\n  Note: ", i.ID, i.Subject, label))
			}
			if r.Outcome == "resolved" || r.Outcome == "commented" {
				note := a.Note
				if note == "" {
					note = "none"
				}
				feedbackAnnotations = append(feedbackAnnotations, a.Annotations)
				notes = append(notes, note)
				answeredIDs = append(answeredIDs, i.ID)
				receiptIDs = append(receiptIDs, a.ActionID)
				if durable {
					receipt := HeraBoardFYIActionReceipt{HeraBoardFYIAction: HeraBoardFYIAction{ActionID: a.ActionID, ItemID: a.ItemID, ExpectedUpdatedAt: a.ExpectedUpdatedAt, Action: a.Action, Note: a.Note, Choice: a.Choice, Annotations: a.Annotations}, RoleID: roleID, Fingerprint: fingerprint, Outcome: r.Outcome, Item: i, CreatedAt: time.Now().UTC(), AnnotationOutcomes: r.AnnotationOutcomes}
					raw, e = json.Marshal(receipt)
					if e != nil {
						return nil, e
					}
					_, e = tx.Exec(`INSERT INTO hera_board_fyi_receipts(role_id,action_id,fingerprint,receipt) VALUES(?,?,?,?)`, roleID, a.ActionID, fingerprint, string(raw))
					if e != nil {
						return nil, e
					}
				}
			}
		}
		results = append(results, r)
	}
	if len(sections) > 0 {
		rows, e := tx.Query(`SELECT id,kind FROM hera_board_items WHERE role_id=? AND resolved=0 ORDER BY id`, roleID)
		if e != nil {
			return nil, e
		}
		var open, openFYIs []string
		for rows.Next() {
			var id int64
			var kind string
			if e = rows.Scan(&id, &kind); e != nil {
				rows.Close()
				return nil, e
			}
			if kind == "decision" {
				open = append(open, fmt.Sprintf("#%d", id))
			} else if kind == "fyi" {
				openFYIs = append(openFYIs, fmt.Sprintf("#%d", id))
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		tldr := fmt.Sprintf("Aaron answered %d board decisions", answeredCount)
		if commentCount > 0 {
			tldr += fmt.Sprintf("; commented on %d open decisions", commentCount)
		}
		head := tldr + ". Act on these; each is already resolved.\n\n"
		if commentCount > 0 {
			head = tldr + ". Act on answers and address every annotation; comment-only decisions remain open.\n\n"
		}
		tail := "\n\nStill open: " + strings.Join(open, ", ") + "\nStill open FYIs: " + strings.Join(openFYIs, ", ")
		budget := (64*1024 - len(head) - len(tail) - len(strings.Join(sections, "\n\n"))) / len(sections)
		for n := range sections {
			note := notes[n]
			if len(feedbackAnnotations[n]) > 0 {
				note, e = boardFeedback(note, feedbackAnnotations[n], budget, roleID, receiptIDs[n])
				if e != nil {
					return nil, e
				}
				sections[n] += note
				continue
			}
			if len(note) > budget {
				marker := fmt.Sprintf("… [truncated — full note on board item #%d; hera_board_list include_resolved=true]", answeredIDs[n])
				if receiptIDs[n] != "" {
					marker = fmt.Sprintf("… [truncated — full feedback: authenticated GET /api/hera/roles/%d/board/fyi-actions/%s]", roleID, receiptIDs[n])
				}
				limit := budget - len(marker)
				if limit < 0 {
					return nil, ErrHeraBoardInvalid
				}
				for limit > 0 && !utf8.ValidString(note[:limit]) {
					limit--
				}
				note = note[:limit] + marker
			}
			sections[n] += note
		}
		body := head + strings.Join(sections, "\n\n") + tail
		_, e = tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,?)`, roleID, roleID, body, tldr, now, HeraDeliveryPending)
		if e != nil {
			return nil, e
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return results, nil
}
