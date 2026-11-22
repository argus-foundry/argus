package hera

import (
	"log/slog"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// orientationHeading opens the recycle seed's standing-orientation section
// (keep-coordinator-orientation-on-recycle D2). These are rules that still
// apply, unlike the "background only" original prompt that follows.
const orientationHeading = "## Standing coordinator orientation (still in force)"

// orientationCheckInDoneNote precedes a sub-coordinator plan node's re-sent
// check-in text so a recycled session does not re-enter a go/wait handshake
// whose "go" will never be re-sent.
const orientationCheckInDoneNote = "Your prior session already completed the launch check-in below; do not check in again or wait for a new 'go' unless your handoff note says you were still waiting for one."

// OrientationStore is the narrow surface CoordinatorOrientation needs. Both
// *db.DB and the MCP server's HeraStore satisfy it.
type OrientationStore interface {
	HeraOrchestrator(id int64) (*db.HeraOrchestrator, error)
	HeraRole(id int64) (*db.HeraRole, error)
	ListHeraRolesByKind(orchID int64, kind db.HeraRoleKind) ([]*db.HeraRole, error)
	ListHeraLiveBindingsByTask(taskID string) ([]*db.HeraBinding, error)
}

// CoordinatorOrientation returns the standing orientation for a coordinator
// role bound to taskID, derived from its CURRENT live structure (never from
// the task prompt, which every recycle overwrites):
//
//   - not a coordinator → "".
//   - the task also holds a live worker binding in another orchestrator (the
//     bridge shape: a kind=subcoord plan node, or a self-promoted worker) →
//     HeraSubCoordinatorOrientation, plus the check-in text when that parent
//     role is a kind=subcoord plan node.
//   - otherwise (top-level) → HeraCoordinatorOrientation.
//
// Fail-open: an unresolvable orchestrator yields "" and an unlistable binding
// set falls back to the top-level text; both are logged, neither errors.
func CoordinatorOrientation(store OrientationStore, role *db.HeraRole, taskID string) string {
	if role == nil || role.Kind != db.HeraKindCoordinator {
		return ""
	}
	orch, err := store.HeraOrchestrator(role.OrchestratorID)
	if err != nil || orch == nil {
		logOrientation("orientation skipped: resolve orchestrator", role.ID, err)
		return ""
	}
	bindings, err := store.ListHeraLiveBindingsByTask(taskID)
	if err != nil {
		logOrientation("orientation fallback to top-level: list bindings", role.ID, err)
		return agent.HeraCoordinatorOrientation(orch.Name)
	}
	for _, b := range bindings {
		if b.OrchestratorID == role.OrchestratorID {
			continue
		}
		parentRole, rErr := store.HeraRole(b.RoleID)
		if rErr != nil || parentRole == nil || parentRole.Kind != db.HeraKindWorker {
			continue
		}
		parentOrch, oErr := store.HeraOrchestrator(b.OrchestratorID)
		if oErr != nil || parentOrch == nil {
			continue
		}
		out := agent.HeraSubCoordinatorOrientation(orch.Name, parentOrch.Name, role.Name)
		if parentRole.NodeKind == db.HeraNodeKindSubCoord {
			parentCoord := "coord" // same default the gater uses
			if coords, cErr := store.ListHeraRolesByKind(parentOrch.ID, db.HeraKindCoordinator); cErr == nil && len(coords) > 0 {
				parentCoord = coords[0].Name
			}
			out += "\n\n" + orientationCheckInDoneNote + "\n\n" + agent.HeraCheckInOrientation(parentOrch.Name, parentCoord)
		}
		return out
	}
	return agent.HeraCoordinatorOrientation(orch.Name)
}

func logOrientation(msg string, roleID int64, err error) {
	uxlog.Log("[recycle] %s role=%d: %v", msg, roleID, err)
	slog.Warn("[recycle] "+msg, "role", roleID, "err", err)
}
