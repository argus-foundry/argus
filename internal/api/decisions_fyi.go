package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

var fyiActionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Six wire bytes per decoded byte covers JSON escaping. Context, paths and
// hashes are bounded separately; a whole batch remains bounded before decoding.
const boardResponseBodyMaxBytes = 16 * (6*(21*4096+20*1024) + 4096)

func initialDecisionRole(role int64) *int64 {
	if role == 0 {
		return nil
	}
	return &role
}

func (s *Server) handleAggregateDecisionLink(w http.ResponseWriter, r *http.Request) {
	if origin := authOrigin(r); origin != "master" && origin != "device" {
		writeErr(w, 403, "master or device token required", nil)
		return
	}
	if r.ContentLength != 0 {
		var p map[string]any
		if !decodeDecisionJSON(w, r, &p, 2048) {
			return
		}
		if p == nil || len(p) != 0 {
			writeCodedErr(w, 400, "invalid_request", "expected empty object")
			return
		}
	}
	rows, err := s.decisionNavigation()
	if err != nil {
		uxlog.Log("[decide] aggregate launch failed")
		writeErr(w, 500, "boards unavailable", nil)
		return
	}
	var role int64
	if len(rows) > 0 {
		role = rows[0].RoleID
	}
	for _, row := range rows {
		if row.Decisions+row.FYIs+row.Actions+row.Todos > 0 {
			role = row.RoleID
			break
		}
	}
	ticket, expires, err := s.decisions.mint(role, false)
	if err != nil {
		uxlog.Log("[decide] aggregate mint failed")
		writeErr(w, 503, "decision link unavailable", nil)
		return
	}
	uxlog.Log("[decide] aggregate minted initial=%d roles=%d", role, len(rows))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"path": "/decide", "ticket": ticket, "expires_at": expires})
}

func (s *Server) handleFYIActions(w http.ResponseWriter, r *http.Request) {
	role, ok := s.decisionRole(w, r)
	if !ok {
		return
	}
	var p struct {
		Actions []db.HeraBoardFYIAction `json:"actions"`
	}
	if !decodeDecisionJSON(w, r, &p, boardResponseBodyMaxBytes) {
		return
	}
	valid := len(p.Actions) > 0 && len(p.Actions) <= 16
	ids := map[string]bool{}
	items := map[int64]bool{}
	for _, a := range p.Actions {
		if !fyiActionID.MatchString(a.ActionID) || ids[a.ActionID] || items[a.ItemID] || a.ItemID <= 0 || a.ExpectedUpdatedAt.IsZero() || len(a.Note) > 4096 || !utf8.ValidString(a.Note) || !db.ValidBoardAnnotationEnvelope(a.Annotations) || (a.Action == "dismiss" && a.Note != "") {
			valid = false
		}
		ids[a.ActionID] = true
		items[a.ItemID] = true
	}
	if !valid {
		uxlog.Log("[decide] invalid FYI envelope role=%d", role)
		writeCodedErr(w, 400, "invalid_request", "FYI actions require 1-16 unique IDs/items, revisions, notes up to 4 KiB, and at most 20 valid annotations with unique annotation_id values and comments up to 4 KiB")
		return
	}
	results, err := s.db.FYIHeraBoardItems(role, p.Actions)
	if err != nil {
		uxlog.Log("[decide] FYI batch failed role=%d", role)
		writeErr(w, 500, "FYI actions unavailable", nil)
		return
	}
	count, _, err := s.db.HeraBoardUnread(role)
	if err != nil {
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	for _, result := range results {
		uxlog.Log("[decide] FYI role=%d item=%d outcome=%s", role, result.ItemID, result.Outcome)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"results": results, "unread_count": count})
}

func (s *Server) handleFYIReceipt(w http.ResponseWriter, r *http.Request) {
	if origin := authOrigin(r); origin != "master" && origin != "device" {
		writeErr(w, 403, "master or device token required", nil)
		return
	}
	role, parseErr := strconv.ParseInt(r.PathValue("role_id"), 10, 64)
	if parseErr != nil || role <= 0 {
		writeErr(w, 400, "invalid role id", nil)
		return
	}
	receipt, err := s.db.HeraBoardFYIReceipt(role, r.PathValue("action_id"))
	if errors.Is(err, db.ErrHeraBoardNotFound) {
		writeErr(w, 404, "receipt not found", nil)
		return
	}
	if err != nil {
		uxlog.Log("[decide] receipt failed role=%d", role)
		writeErr(w, 500, "receipt unavailable", nil)
		return
	}
	uxlog.Log("[decide] receipt read role=%d", role)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, receipt)
}
