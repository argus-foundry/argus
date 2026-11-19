package db

import (
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
	ItemID            int64     `json:"item_id"`
	ExpectedUpdatedAt time.Time `json:"expected_updated_at"`
	Choice            string    `json:"choice"`
	Note              string    `json:"note"`
}

type HeraBoardAnswerResult struct {
	ItemID  int64          `json:"item_id"`
	Outcome string         `json:"outcome"`
	Item    *HeraBoardItem `json:"item,omitempty"`
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
		if r.Outcome == "resolved" {
			d.emitBoardChanged(roleID, r.ItemID, "resolved")
		}
	}
	return results, nil
}

func (d *DB) answerHeraBoardItems(roleID int64, answers []HeraBoardAnswer) ([]HeraBoardAnswerResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
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
	now := formatTime(time.Now())
	for _, a := range answers {
		r := HeraBoardAnswerResult{ItemID: a.ItemID, Outcome: "invalid"}
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
			r.Outcome = "already_resolved"
		case a.ExpectedUpdatedAt.IsZero() || len(a.Note) > 4096:
		case !i.UpdatedAt.Equal(a.ExpectedUpdatedAt):
			r.Outcome = "conflict"
			r.Item = i
		default:
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
			if label != "" {
				_, e = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by='human',resolution_choice=?,resolution_note=?,updated_at=? WHERE role_id=? AND id=?`, a.Choice, a.Note, now, roleID, a.ItemID)
				if e != nil {
					return nil, e
				}
				r.Outcome = "resolved"
				note := a.Note
				if note == "" {
					note = "none"
				}
				sections = append(sections, fmt.Sprintf("#%d %s\n  Choice: %s\n  Note: ", i.ID, i.Subject, label))
				notes = append(notes, note)
				answeredIDs = append(answeredIDs, i.ID)
			}
		}
		results = append(results, r)
	}
	if len(sections) > 0 {
		rows, e := tx.Query(`SELECT id FROM hera_board_items WHERE role_id=? AND kind='decision' AND resolved=0 ORDER BY id`, roleID)
		if e != nil {
			return nil, e
		}
		var open []string
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			open = append(open, fmt.Sprintf("#%d", id))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		tldr := fmt.Sprintf("Aaron answered %d board decisions", len(sections))
		head := tldr + ". Act on these; each is already resolved.\n\n"
		tail := "\n\nStill open: " + strings.Join(open, ", ")
		budget := (64*1024 - len(head) - len(tail) - len(strings.Join(sections, "\n\n"))) / len(sections)
		for n := range sections {
			note := notes[n]
			if len(note) > budget {
				marker := fmt.Sprintf("… [truncated — full note on board item #%d; hera_board_list include_resolved=true]", answeredIDs[n])
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
	return results, nil
}
