// Package plan is the tview-free projection of a Hera orchestrator's plan DAG:
// its planned and live worker roles as nodes and its blocking edges as
// dependencies. The TUI plan pane and GET /api/hera/orchestrators/{id}/plan
// both call it so the two surfaces cannot drift.
package plan

import (
	"encoding/json"
	"fmt"

	"github.com/drn/argus/internal/db"
	heramodel "github.com/drn/argus/internal/hera/model"
	"github.com/drn/argus/internal/model"
)

type State string

const (
	StatePlanned    State = "planned"
	StatePending    State = "pending"
	StateInProgress State = "in_progress"
	StateInReview   State = "in_review"
	StateDone       State = "done"
	StateFailed     State = "failed"
	StateCancelled  State = "cancelled"
)

// Node is one plan node. Role points at the already-built role view so callers
// can render extra per-node detail without a second lookup.
type Node struct {
	Role         *heramodel.RoleView
	RoleID       int64
	TaskID       string
	Name         string
	State        State
	Planned      bool
	Cancelled    bool
	Prompt       string
	Archetype    string
	ChildOrchID  int64
	BridgeTarget bool
}

// Edge is a dependency: DependentRoleID waits on BlockerRoleID.
type Edge struct {
	BlockerRoleID   int64
	DependentRoleID int64
}

// WireID is a node's identity on the wire. It is derived from the role, so it
// is stable across materialization (planned -> bound task).
func WireID(roleID int64) string { return fmt.Sprintf("role:%d", roleID) }

// TaskFailed reports whether a task result JSON carries {"failed":true}.
func TaskFailed(raw string) bool {
	if raw == "" {
		return false
	}
	var v struct {
		Failed bool `json:"failed"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return false
	}
	return v.Failed
}

// StateOf maps a role to its plan state. Cancelled wins over everything, then
// planned, then a failed task result, then the bound task's workflow status.
func StateOf(r *heramodel.RoleView) State {
	if r.Cancelled {
		return StateCancelled
	}
	if r.Planned {
		return StatePlanned
	}
	if TaskFailed(r.TaskResult) {
		return StateFailed
	}
	switch r.TaskStatus {
	case model.StatusComplete.String():
		return StateDone
	case model.StatusInReview.String():
		return StateInReview
	case model.StatusInProgress.String():
		return StateInProgress
	default:
		return StatePending
	}
}

// Project projects an orchestrator's plan. Coordinator and archived roles are
// not nodes; an edge survives only when both endpoints are nodes. bridge maps a
// coordinator bridge task to the orchestrator it coordinates (Model.BridgeIndex)
// and may be nil, in which case no node has a child orchestrator.
func Project(orch *heramodel.OrchView, bridge map[string]*heramodel.OrchView) ([]Node, []Edge) {
	if orch == nil {
		return nil, nil
	}
	present := make(map[int64]struct{}, len(orch.Roles))
	nodes := make([]Node, 0, len(orch.Roles))
	for i := range orch.Roles {
		r := &orch.Roles[i]
		if r.Kind != db.HeraKindWorker || r.Archived {
			continue
		}
		present[r.RoleID] = struct{}{}
		taskID := r.TaskID
		if taskID == "" {
			taskID = r.BridgeTaskID
		}
		n := Node{
			Role:      r,
			RoleID:    r.RoleID,
			TaskID:    taskID,
			Name:      r.Name,
			State:     StateOf(r),
			Planned:   r.Planned,
			Cancelled: r.Cancelled,
			Prompt:    r.Prompt,
			Archetype: r.Archetype,
		}
		if bridge != nil && r.BridgeTaskID != "" {
			if child := bridge[r.BridgeTaskID]; child != nil && child.ID != orch.ID {
				n.ChildOrchID = child.ID
				n.BridgeTarget = true
			}
		}
		nodes = append(nodes, n)
	}
	edges := make([]Edge, 0, len(orch.Blocks))
	for _, b := range orch.Blocks {
		if _, ok := present[b.BlockerRoleID]; !ok {
			continue
		}
		if _, ok := present[b.BlockedRoleID]; !ok {
			continue
		}
		edges = append(edges, Edge{BlockerRoleID: b.BlockerRoleID, DependentRoleID: b.BlockedRoleID})
	}
	return nodes, edges
}

// IsAuthored reports whether a projection is an authored plan: at least one
// planned node or at least one edge. Live roles alone are a roster, not a plan.
func IsAuthored(nodes []Node, edges []Edge) bool {
	if len(edges) > 0 {
		return true
	}
	for _, n := range nodes {
		if n.Planned {
			return true
		}
	}
	return false
}
