package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// handleRecordSessionID backs the Codex SessionStart hook (`argus
// codex-session-hook`): it records the backend-minted session ID the moment it
// exists. Fill-only and Codex-only (agent.RecordHookSessionID), so a repeated or
// stray report never replaces a conversation.
func (s *Server) handleRecordSessionID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	recorded, err := agent.RecordHookSessionID(s.db, id, req.SessionID)
	if err != nil {
		uxlog.Log("[api] session-id report rejected task=%s: %v", id, err)
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "", err)
			return
		}
		writeErr(w, http.StatusBadRequest, "", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": recorded})
}
