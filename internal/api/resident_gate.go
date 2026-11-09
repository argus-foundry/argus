package api

import (
	"fmt"
	"net/http"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// residentGate wraps a /api/tasks/{id}... handler so the resident agent is
// invisible to it (add-resident-agent). With an empty verb a request for the
// resident id is a plain 404 — the resident is not a task. With a verb it is a
// 409 naming the resident, the explicit rejection for lifecycle operations
// (complete, archive, delete, fork, set-status, account move, ...). Requests
// for any other id, and any lookup that fails, pass through untouched.
func (s *Server) residentGate(next http.HandlerFunc, verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id := r.PathValue("id"); id != "" && s.db != nil {
			if t, err := s.db.Get(id); err == nil && t.IsResident() {
				if verb == "" {
					writeErr(w, http.StatusNotFound, "task not found", nil)
					return
				}
				uxlog.Log("[resident] rejected REST %s %s: cannot %s the resident", r.Method, r.URL.Path, verb)
				writeErr(w, http.StatusConflict, fmt.Sprintf("cannot %s the resident agent: it is not a task and is not lifecycle-managed", verb), nil)
				return
			}
		}
		next(w, r)
	}
}
