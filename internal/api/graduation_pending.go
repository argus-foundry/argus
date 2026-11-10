package api

import (
	"net/http"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// refuseGraduationPending writes 409 and reports true when taskID is a
// graduate whose restart is still pending, or 500 when that state cannot be
// read (fail closed). Only the daemon's graduation watcher may restart such a
// session; a runtime change or session switch would also break the resume it
// relies on.
func (s *Server) refuseGraduationPending(w http.ResponseWriter, taskID, what string) bool {
	pending, err := s.db.GraduationPendingErr(taskID)
	if err == nil && !pending {
		return false
	}
	uxlog.Log("[resident] %s refused: graduation pending task=%s err=%v", what, taskID, err)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "graduation check failed", err)
		return true
	}
	writeErr(w, http.StatusConflict, "", db.ErrGraduationPending)
	return true
}
