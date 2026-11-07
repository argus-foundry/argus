package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/hera"
	"github.com/drn/argus/internal/uxlog"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SetHeraLifecycle wires the shared bounce/recycle service. Optional: nil makes the routes return 503.
func (s *Server) SetHeraLifecycle(l *hera.LifecycleService) { s.heraLifecycle = l }

type heraLifecycleReq struct {
	RequestID string `json:"request_id"`
	Force     *bool  `json:"force"`
}

func decodeLifecycleReq(w http.ResponseWriter, r *http.Request, v *heraLifecycleReq) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, heraMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || !uuidRe.MatchString(v.RequestID) {
		writeErr(w, http.StatusBadRequest, "request_id must be a UUID and the body must contain only known fields", nil)
		return false
	}
	return true
}

func (s *Server) lifecycleRoute(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if s.heraLifecycle == nil {
		writeErr(w, http.StatusServiceUnavailable, "hera lifecycle unavailable", nil)
		return 0, false
	}
	return heraPathID(w, r)
}

func (s *Server) handleHeraBounce(w http.ResponseWriter, r *http.Request) {
	roleID, ok := s.lifecycleRoute(w, r)
	if !ok {
		return
	}
	var req heraLifecycleReq
	if !decodeLifecycleReq(w, r, &req) || req.Force != nil {
		if req.Force != nil {
			writeErr(w, http.StatusBadRequest, "force is not accepted for bounce", nil)
		}
		return
	}
	op, err := s.heraLifecycle.Bounce(req.RequestID, roleID)
	s.writeLifecycle(w, "bounce", roleID, op, err)
}

func (s *Server) handleHeraRecycle(w http.ResponseWriter, r *http.Request) {
	roleID, ok := s.lifecycleRoute(w, r)
	if !ok {
		return
	}
	var req heraLifecycleReq
	if !decodeLifecycleReq(w, r, &req) {
		return
	}
	if req.Force == nil || !*req.Force {
		writeErr(w, http.StatusBadRequest, "force must be true", nil)
		return
	}
	op, err := s.heraLifecycle.ForceRecycle(req.RequestID, roleID, false)
	s.writeLifecycle(w, "recycle", roleID, op, err)
}

func (s *Server) handleHeraLifecycleGet(w http.ResponseWriter, r *http.Request) {
	roleID, ok := s.lifecycleRoute(w, r)
	if !ok {
		return
	}
	opID := r.PathValue("op")
	if !uuidRe.MatchString(opID) {
		writeErr(w, http.StatusBadRequest, "invalid operation id", nil)
		return
	}
	op, err := s.heraLifecycle.Get(roleID, opID)
	if err != nil {
		s.writeLifecycle(w, "get", roleID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, hera.LifecycleView(op))
}

func (s *Server) writeLifecycle(w http.ResponseWriter, verb string, roleID int64, op *db.Operation, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, hera.LifecycleView(op))
		return
	case errors.Is(err, db.ErrHeraNotFound), errors.Is(err, db.ErrOperationNotFound):
		writeErr(w, http.StatusNotFound, "not found", nil)
	case errors.Is(err, hera.ErrLifecycleWrongKind), errors.Is(err, hera.ErrLifecycleStaleBinding),
		errors.Is(err, hera.ErrLifecycleNoSession), errors.Is(err, hera.ErrLifecycleActive),
		errors.Is(err, hera.ErrLifecycleRequestReuse):
		writeErr(w, http.StatusConflict, err.Error(), nil)
	default:
		uxlog.Log("[api-hera] lifecycle %s role=%d failed: %v", verb, roleID, err)
		writeErr(w, http.StatusInternalServerError, "could not record the operation", nil)
	}
}
