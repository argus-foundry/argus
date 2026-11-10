package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
)

var residentGraduateTool = Tool{Name: "hera_graduate_worker", Description: "Resident only, asks Aaron first: graduate a live Claude worker into an independent top-level coordinator, preserving its conversation and worktree. Refuses busy, unresumable and non-Claude workers. Restart waits until idle without forcing.", InputSchema: map[string]interface{}{
	"type": "object", "properties": map[string]interface{}{"role": map[string]interface{}{"type": "string", "description": "Worker name or numeric role id"}}, "required": []string{"role"},
}}

// SetHeraGraduationPreflight wires daemon session/transcript validation. The
// restart itself is driven by durable task metadata, never a request goroutine.
func (s *Server) SetHeraGraduationPreflight(check func(*model.Task) error) {
	s.heraGraduationPreflight = check
}

func (s *Server) toolHeraGraduateWorker(id interface{}, args json.RawMessage, caller string) *Response {
	if !s.boundResident(caller) {
		return residentToolError(id, "hera_graduate_worker is resident only (connection-bound caller required)")
	}
	// Delete cannot validate stale worker membership while graduation commits.
	// This scope deliberately ends before the daemon's idle/restart loop.
	residentWorkerMutationMu.Lock()
	defer residentWorkerMutationMu.Unlock()
	d, ok := s.taskDB.(*db.DB)
	if !ok || !s.heraEnabled() || s.heraGraduationPreflight == nil {
		return residentToolError(id, "resident graduation not configured")
	}
	var p struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return residentToolError(id, err.Error())
	}
	o, err := d.HeraOrchestratorByName(db.ResidentOrchestratorName)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	own, err := d.HeraLiveBindingByTaskAndOrchestrator(caller, o.ID)
	if err != nil {
		return residentToolError(id, "resident standing binding unavailable")
	}
	r, err := d.HeraRoleByName(o.ID, p.Role)
	if err != nil {
		if rid, e := strconv.ParseInt(p.Role, 10, 64); e == nil {
			r, err = d.HeraRole(rid)
		}
	}
	if err != nil || r == nil || r.OrchestratorID != o.ID || r.Kind != db.HeraKindWorker || r.NukedAt != nil || r.ArchivedAt != nil || r.ID == own.RoleID {
		return residentToolError(id, "target must be a live worker of the resident orchestrator, never itself")
	}
	b, err := d.HeraLiveBindingByRole(r.ID)
	if err != nil {
		return residentToolError(id, "target is not a live resident worker (possibly graduated)")
	}
	task, err := d.Get(b.ArgusTaskID)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	if task.IsResident() || task.ID == caller {
		return residentToolError(id, "graduation must never target the resident itself")
	}
	backend, err := agent.ResolveBackend(task, d.Config())
	if err != nil || !agent.IsClaudeBackend(backend.Command) {
		return residentToolError(id, "only Claude workers can graduate; Codex is ineligible. Offer Aaron a fresh Claude worker seeded with a handoff, only on his yes")
	}
	if task.SessionID == "" {
		return residentToolError(id, "worker has no resumable session id")
	}
	if err = s.heraGraduationPreflight(task); err != nil {
		return residentToolError(id, err.Error())
	}
	result, err := d.GraduateHeraWorker(b.ID)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	return toolResult(id, fmt.Sprintf("Graduated %s as independent coordinator role %d; graduation pending restart. The daemon will resume the same conversation once idle, never forcing.", r.Name, result.NewRole.ID))
}
