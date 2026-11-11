package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type routingPreviewReq struct {
	Project   string `json:"project"`
	Backend   string `json:"backend"`
	Archetype string `json:"archetype"`
	Profile   string `json:"profile"`
}

type routingPreviewResp struct {
	Backend       string   `json:"backend"`
	Kind          string   `json:"kind"`
	Models        []string `json:"models"`
	ModelPolicy   string   `json:"model_policy"`
	DefaultModel  *string  `json:"default_model"`
	RoutingReason string   `json:"routing_reason"`
	Advisory      bool     `json:"advisory"`
}

// handleRoutingPreview serves POST /api/routing/preview: the advisory,
// read-only backend/model resolution task creation would perform.
func (s *Server) handleRoutingPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req routingPreviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		uxlog.Log("[routing] preview rejected 400: invalid JSON")
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Project == "" {
		uxlog.Log("[routing] preview rejected 400: project required")
		writeCodedErr(w, http.StatusBadRequest, "invalid_request", "project is required")
		return
	}
	p, err := agent.PreviewRouting(s.db.Config(), req.Project, req.Backend, req.Archetype, req.Profile)
	switch {
	case errors.Is(err, agent.ErrPreviewProjectNotFound):
		uxlog.Log("[routing] preview rejected 404 project=%q", req.Project)
		writeCodedErr(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	case errors.Is(err, agent.ErrPreviewBackendUnavailable):
		uxlog.Log("[routing] preview rejected 422 project=%q backend=%q", req.Project, req.Backend)
		writeCodedErr(w, http.StatusUnprocessableEntity, "backend_unavailable", "no valid backend candidate")
		return
	case err != nil:
		uxlog.Log("[routing] preview failed 500: %v", err)
		writeCodedErr(w, http.StatusInternalServerError, "internal_error", "routing resolution failed")
		return
	}
	resp := routingPreviewResp{
		Backend: p.Backend, Kind: p.Kind, Models: p.Models, ModelPolicy: p.ModelPolicy,
		RoutingReason: p.Reason, Advisory: true,
	}
	if p.DefaultModel != "" {
		resp.DefaultModel = &p.DefaultModel
	}
	uxlog.Log("[routing] preview ok project=%q backend=%q reason=%q", req.Project, p.Backend, p.Reason)
	writeJSON(w, http.StatusOK, resp)
}
