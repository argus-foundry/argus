package api

import (
	"net/http"

	heramodel "github.com/drn/argus/internal/hera/model"
	heraplan "github.com/drn/argus/internal/hera/plan"
	"github.com/drn/argus/internal/uxlog"
)

const (
	maxPlanNodes = 2000
	maxPlanEdges = 5000
)

type heraPlanNodeJSON struct {
	ID                  string  `json:"id"`
	RoleID              int64   `json:"role_id"`
	TaskID              *string `json:"task_id"`
	Name                string  `json:"name"`
	State               string  `json:"state"`
	Planned             bool    `json:"planned"`
	Cancelled           bool    `json:"cancelled"`
	Prompt              string  `json:"prompt"`
	Archetype           string  `json:"archetype"`
	ChildOrchestratorID *int64  `json:"child_orchestrator_id"`
}

type heraPlanEdgeJSON struct {
	BlockerID   string `json:"blocker_id"`
	DependentID string `json:"dependent_id"`
}

type heraPlanJSON struct {
	OrchestratorID int64              `json:"orchestrator_id"`
	Authored       bool               `json:"authored"`
	Nodes          []heraPlanNodeJSON `json:"nodes"`
	Edges          []heraPlanEdgeJSON `json:"edges"`
}

// handleHeraPlan returns one orchestrator's authored plan graph. Read-only; it
// shares heraplan.Project with the TUI plan pane. A graph over the size caps is
// refused with 413 rather than truncated.
func (s *Server) handleHeraPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := heraPathID(w, r)
	if !ok {
		return
	}
	runningSet, idleSet, needsInputSet := s.sessionStateMaps()
	m, err := heramodel.BuildModel(s.db, needsInputSet, idleSet, runningSet, nil)
	if err != nil {
		uxlog.Log("[heraplan] build model failed orch=%d: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "failed to build hera model", err)
		return
	}
	orch := m.OrchByID(id)
	if orch == nil {
		writeErr(w, http.StatusNotFound, "orchestrator not found", nil)
		return
	}
	nodes, edges := heraplan.Project(orch, m.BridgeIndex())
	if len(nodes) > maxPlanNodes || len(edges) > maxPlanEdges {
		uxlog.Log("[heraplan] orch=%d too large nodes=%d edges=%d", id, len(nodes), len(edges))
		writeErr(w, http.StatusRequestEntityTooLarge, "plan graph too large", nil)
		return
	}
	out := heraPlanJSON{
		OrchestratorID: id,
		Authored:       heraplan.IsAuthored(nodes, edges),
		Nodes:          []heraPlanNodeJSON{},
		Edges:          []heraPlanEdgeJSON{},
	}
	if out.Authored {
		for _, n := range nodes {
			nj := heraPlanNodeJSON{
				ID:        heraplan.WireID(n.RoleID),
				RoleID:    n.RoleID,
				TaskID:    strPtr(n.TaskID),
				Name:      n.Name,
				State:     string(n.State),
				Planned:   n.Planned,
				Cancelled: n.Cancelled,
				Prompt:    n.Prompt,
				Archetype: n.Archetype,
			}
			if n.ChildOrchID != 0 {
				c := n.ChildOrchID
				nj.ChildOrchestratorID = &c
			}
			out.Nodes = append(out.Nodes, nj)
		}
		for _, e := range edges {
			out.Edges = append(out.Edges, heraPlanEdgeJSON{
				BlockerID:   heraplan.WireID(e.BlockerRoleID),
				DependentID: heraplan.WireID(e.DependentRoleID),
			})
		}
	}
	uxlog.Log("[heraplan] orch=%d authored=%t nodes=%d edges=%d", id, out.Authored, len(out.Nodes), len(out.Edges))
	writeJSON(w, http.StatusOK, out)
}
