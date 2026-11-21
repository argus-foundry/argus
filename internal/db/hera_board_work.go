package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// HeraBoardMetadataPatch distinguishes omitted fields from explicit clearing.
// These board todos are unrelated to the Things 3 todo backend.
type HeraBoardMetadataPatch struct {
	Blocking       *bool   `json:"blocking,omitempty"`
	Trigger        *string `json:"trigger,omitempty"`
	TriggerPending *bool   `json:"trigger_pending,omitempty"`
	Command        *string `json:"command,omitempty"`
}

type boardWorkMetadata struct {
	Blocking       bool
	Trigger        string
	TriggerPending bool
	Command        string
}

func nextBoardRevision(previous time.Time) time.Time {
	now := time.Now().UTC()
	if !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}

// AttentionUnread is the shared human attention predicate. Team work remains
// visible on its board but never rings Aaron's mailbox.
func (i *HeraBoardItem) AttentionUnread() bool {
	return !i.Resolved && !i.Read && !i.TriggerPending && i.Kind != "todo"
}

func boardMetadata(kind string, before *HeraBoardItem, p HeraBoardMetadataPatch) (boardWorkMetadata, error) {
	var m boardWorkMetadata
	if before != nil {
		m = boardWorkMetadata{before.Blocking, before.Trigger, before.TriggerPending, before.Command}
	}
	if kind != "action" {
		m.Blocking, m.Command = false, ""
		if p.Blocking != nil || p.Command != nil {
			return m, ErrHeraBoardInvalid
		}
	}
	if kind != "action" && kind != "todo" {
		m.Trigger, m.TriggerPending = "", false
		if p.Trigger != nil || p.TriggerPending != nil {
			return m, ErrHeraBoardInvalid
		}
	}
	if kind == "action" && (before == nil || before.Kind != "action") && p.Blocking == nil {
		return m, ErrHeraBoardInvalid
	}
	if p.Blocking != nil {
		m.Blocking = *p.Blocking
	}
	if p.Trigger != nil {
		m.Trigger = *p.Trigger
		if before == nil {
			m.TriggerPending = m.Trigger != ""
		}
	}
	if p.TriggerPending != nil {
		m.TriggerPending = *p.TriggerPending
	}
	if p.Command != nil {
		m.Command = *p.Command
	}
	if !utf8.ValidString(m.Trigger) || !utf8.ValidString(m.Command) || len(m.Trigger) > 1024 || len(m.Command) > 16*1024 || (m.Trigger != "" && strings.TrimSpace(m.Trigger) == "") || (m.Command != "" && strings.TrimSpace(m.Command) == "") || (m.TriggerPending && (m.Trigger == "" || m.Blocking)) {
		return m, ErrHeraBoardInvalid
	}
	return m, nil
}

func (d *DB) PostHeraBoardItemWithMetadata(roleID int64, kind, subject, body string, p HeraBoardMetadataPatch, structure ...*HeraBoardStructure) (*HeraBoardItem, error) {
	i, err := d.postHeraBoardItem(roleID, kind, subject, body, p, structure...)
	if err == nil {
		d.emitBoardChanged(roleID, i.ID, "posted")
	}
	return i, err
}

func (d *DB) UpdateHeraBoardItemWithMetadata(roleID, id int64, kind, subject, body string, p HeraBoardMetadataPatch, structure ...*HeraBoardStructure) (*HeraBoardItem, error) {
	i, changed, err := d.updateHeraBoardItem(roleID, id, kind, subject, body, p, structure...)
	if err == nil && changed {
		d.emitBoardChanged(roleID, i.ID, "updated")
	}
	return i, err
}

func boardWorkOutcome(kind, outcome string) bool {
	return kind == "action" && outcome == "done" || kind == "todo" && (outcome == "done" || outcome == "dropped")
}

func writeBoardWorkNotice(tx *sql.Tx, roleID int64, i *HeraBoardItem, outcome, note string) error {
	tldr := fmt.Sprintf("Aaron (board) %s %s #%d", outcome, i.Kind, i.ID)
	body := tldr + ": " + i.Subject
	if note != "" {
		body += "\n\n" + note
	}
	_, err := tx.Exec(`INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,delivery_mode) VALUES(?,?,?,?,?,?)`, roleID, roleID, body, tldr, formatTime(i.UpdatedAt), HeraDeliveryPending)
	return err
}

// ResolveHeraBoardWorkItem is the agent's explicit done/dropped path. Human
// completions use the revision-checked, receipt-backed board action batch.
func (d *DB) ResolveHeraBoardWorkItem(roleID, id int64, outcome, note string) error {
	if len(note) > 4096 || !utf8.ValidString(note) {
		return ErrHeraBoardInvalid
	}
	changed := false
	err := d.WithTx(func(tx *sql.Tx) error {
		i, err := scanBoard(tx.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, roleID, id))
		if err != nil {
			return err
		}
		if !boardWorkOutcome(i.Kind, outcome) {
			return ErrHeraBoardInvalid
		}
		if i.Resolved {
			return nil
		}
		i.UpdatedAt = nextBoardRevision(i.UpdatedAt)
		_, err = tx.Exec(`UPDATE hera_board_items SET resolved=1,resolved_by='agent',resolution_choice=?,resolution_note=?,updated_at=? WHERE id=?`, outcome, note, formatTime(i.UpdatedAt), id)
		if err != nil {
			return err
		}
		if err = writeBoardWorkNotice(tx, roleID, i, outcome, note); err != nil {
			return err
		}
		changed = true
		return nil
	})
	uxlog.Log("[board] work resolve role=%d item=%d outcome=%s changed=%v err=%v", roleID, id, outcome, changed, err)
	if err == nil && changed {
		d.emitBoardChanged(roleID, id, "resolved")
	}
	return err
}
