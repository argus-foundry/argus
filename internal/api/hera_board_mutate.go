package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	boardBodyMaxBytes = 80 * 1024
	// JSON can encode each note byte as six bytes; reserve space for the revision envelope.
	boardDismissBodyMaxBytes = 6*64*1024 + 1024
)

func writeCodedErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// boardTarget validates the path and ownership shared by both board mutations.
func (s *Server) boardTarget(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	roleID, err := strconv.ParseInt(r.PathValue("role_id"), 10, 64)
	itemID, err2 := strconv.ParseInt(r.PathValue("item_id"), 10, 64)
	if err != nil || err2 != nil || roleID <= 0 || itemID <= 0 {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid role or item id")
		return 0, 0, false
	}
	role, err := s.db.HeraRole(roleID)
	if err != nil {
		if errors.Is(err, db.ErrHeraNotFound) {
			writeCodedErr(w, http.StatusNotFound, "board_item_not_found", "board item not found")
		} else {
			uxlog.Log("[board] REST role lookup failed role=%d: %v", roleID, err)
			writeCodedErr(w, http.StatusInternalServerError, "board_write_failed", "board unavailable")
		}
		return 0, 0, false
	}
	if role.Kind != db.HeraKindCoordinator {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "board belongs to a coordinator role")
		return 0, 0, false
	}
	return roleID, itemID, true
}

func parseExpected(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	return t, err == nil && !t.IsZero()
}

func (s *Server) writeBoardResult(w http.ResponseWriter, op string, roleID, itemID int64, item *db.HeraBoardItem, unread int, err error) {
	switch {
	case err == nil:
		uxlog.Log("[board] REST %s ok role=%d item=%d unread=%d", op, roleID, itemID, unread)
		writeJSON(w, http.StatusOK, map[string]any{"item": item, "unread_count": unread})
	case errors.Is(err, db.ErrHeraBoardNotFound):
		writeCodedErr(w, http.StatusNotFound, "board_item_not_found", "board item not found")
	case errors.Is(err, db.ErrHeraBoardConflict):
		writeCodedErr(w, http.StatusConflict, "revision_conflict", "board item changed since it was read")
	case errors.Is(err, db.ErrHeraBoardResolved):
		writeCodedErr(w, http.StatusConflict, "item_resolved", "board item already resolved")
	case errors.Is(err, db.ErrHeraBoardInvalid):
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid board request")
	default:
		uxlog.Log("[board] REST %s failed role=%d item=%d: %v", op, roleID, itemID, err)
		writeCodedErr(w, http.StatusInternalServerError, "board_write_failed", "board write failed")
	}
}

func (s *Server) handleHeraBoardReadState(w http.ResponseWriter, r *http.Request) {
	roleID, itemID, ok := s.boardTarget(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, boardBodyMaxBytes)
	var req struct {
		Read     *bool  `json:"read"`
		Expected string `json:"expected_updated_at"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil || req.Read == nil {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: read is required")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	exp, ok := parseExpected(req.Expected)
	if !ok {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "expected_updated_at must be nonzero RFC3339")
		return
	}
	item, unread, err := s.db.SetHeraBoardItemRead(roleID, itemID, *req.Read, exp)
	s.writeBoardResult(w, "read-state", roleID, itemID, item, unread, err)
}

func (s *Server) handleHeraBoardDismiss(w http.ResponseWriter, r *http.Request) {
	roleID, itemID, ok := s.boardTarget(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, boardDismissBodyMaxBytes)
	var req struct {
		Expected string `json:"expected_updated_at"`
		Note     string `json:"note"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	exp, ok := parseExpected(req.Expected)
	if !ok {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "expected_updated_at must be nonzero RFC3339")
		return
	}
	if len(req.Note) > 64*1024 {
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "note exceeds 64 KiB")
		return
	}
	item, unread, err := s.db.DismissHeraBoardItem(roleID, itemID, exp, req.Note)
	s.writeBoardResult(w, "dismiss", roleID, itemID, item, unread, err)
}
