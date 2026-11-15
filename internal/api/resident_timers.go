package api

import (
	"net/http"

	"github.com/argus-foundry/argus/internal/uxlog"
)

func (s *Server) handleResidentTimers(w http.ResponseWriter, r *http.Request) {
	timers, err := s.db.ResidentTimers()
	if err != nil {
		uxlog.Log("[resident-timer] list failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	uxlog.Log("[resident-timer] list count=%d", len(timers))
	writeJSON(w, http.StatusOK, map[string]any{"timers": timers})
}
