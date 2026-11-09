package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	heraMessagesDefaultLimit = 100
	heraMessagesMaxLimit     = 500
	deletedRoleName          = "(deleted role)"
)

type heraMessageJSON struct {
	ID           int64      `json:"id"`
	Source       string     `json:"source"`
	FromRoleID   int64      `json:"from_role_id"`
	FromName     string     `json:"from_name"`
	ToRoleID     int64      `json:"to_role_id"`
	ToName       string     `json:"to_name"`
	Body         string     `json:"body"`
	Tldr         string     `json:"tldr"`
	InReplyTo    *int64     `json:"in_reply_to"`
	SentAt       time.Time  `json:"sent_at"`
	ReadAt       *time.Time `json:"read_at"`
	DeliveryMode string     `json:"delivery_mode"`
	DeliveredAt  *time.Time `json:"delivered_at"`
}

// stripTermControls mirrors the TUI inbox sanitizer so a hostile message can
// never smuggle terminal escapes to any client that renders it raw.
func stripTermControls(s string, keepNewlines bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNewlines:
			return r
		case r == '\t', r == '\n':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}

// handleTaskHeraMessages is the read-only message history view. It must never
// call HeraInbox/MarkHeraMessagesRead: viewing history is not acknowledging mail.
func (s *Server) handleTaskHeraMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.Get(id); err != nil {
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "", err)
			return
		}
		uxlog.Log("[hera-messages] REST task lookup failed task=%s: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "internal server error", nil)
		return
	}
	q := r.URL.Query()
	limit := heraMessagesDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > heraMessagesMaxLimit {
			writeErr(w, http.StatusBadRequest, "invalid limit (1-500)", nil)
			return
		}
		limit = n
	}
	var before int64
	if v := q.Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid before_id", nil)
			return
		}
		before = n
	}

	msgs, more, err := s.db.HeraMessagesPageForTask(id, limit, before)
	if err != nil {
		uxlog.Log("[hera-messages] REST history failed task=%s: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "internal server error", nil)
		return
	}

	names := map[int64]string{}
	roleName := func(rid int64) string {
		if n, ok := names[rid]; ok {
			return n
		}
		n := deletedRoleName
		if role, err := s.db.HeraRole(rid); err == nil && role != nil {
			n = stripTermControls(role.Name, false)
		}
		names[rid] = n
		return n
	}
	out := make([]heraMessageJSON, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, heraMessageJSON{
			ID: m.ID, Source: "hera",
			FromRoleID: m.FromRoleID, FromName: m.SenderName(roleName(m.FromRoleID)),
			ToRoleID: m.ToRoleID, ToName: roleName(m.ToRoleID),
			Body: stripTermControls(m.Body, true), Tldr: stripTermControls(m.Tldr, false),
			InReplyTo: m.InReplyTo, SentAt: m.SentAt, ReadAt: m.ReadAt,
			DeliveryMode: m.DeliveryMode, DeliveredAt: m.DeliveredAt,
		})
	}
	var next *int64
	if more && len(out) > 0 {
		next = &out[0].ID
	}
	uxlog.Log("[hera-messages] REST history task=%s count=%d more=%v", id, len(out), more)
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "next_before_id": next})
}
