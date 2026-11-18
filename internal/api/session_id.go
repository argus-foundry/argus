package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// handleRecordSessionID backs `argus codex-session-hook`: a startup report fills an empty ID, a "clear" (/new) replaces it.
func (s *Server) handleRecordSessionID(w http.ResponseWriter, r *http.Request) {
	// Master-only: it decides which conversation a task later resumes.
	if requireMaster(w, r) {
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	recorded, err := agent.RecordHookSessionID(s.db, id, req.SessionID, req.Source == "clear")
	if err != nil {
		uxlog.Log("[api] session-id report rejected task=%s: %v", id, err)
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "", err)
			return
		}
		if errors.Is(err, agent.ErrInvalidSessionReport) {
			writeErr(w, http.StatusBadRequest, "", err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "record session id failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": recorded})
}
