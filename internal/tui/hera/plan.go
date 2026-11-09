package hera

import (
	"fmt"

	heramodel "github.com/drn/argus/internal/hera/model"
	heraplan "github.com/drn/argus/internal/hera/plan"

	"github.com/drn/argus/internal/tui/planview"
	"github.com/drn/argus/internal/tui/widget"
	"github.com/drn/argus/internal/uxlog"
)

// heraPlanNodes projects an orchestrator's PLAN DAG — its planned (never-bound)
// and live worker roles as nodes, its hera_blocks blocking edges as dependency
// edges — into the planview widget's input. It is the plan-view parallel to the
// retired heraTreeNodes (which projected the role hierarchy); this projects the
// order-of-ops graph the coordinator authored.
//
// It is a pure function over the already-built heramodel.OrchView (the rail's snapshot,
// with heramodel.OrchView.Blocks populated by heramodel.BuildModel) — no DB read, no I/O — so it is
// safe on the tview thread and trivially testable. A live node carries its
// bound task's status/result (the colour source); a planned node carries
// State=StatePlanned. The projection ALWAYS surfaces every live worker role as a
// node (needed when a plan IS authored — a worker materialized from a planned
// node must still render with its task colour). Whether the result is RENDERED as
// a DAG or as the empty-plan state is the Plan pane's call: rebuildPlan checks
// planIsAuthored and feeds the widget an EMPTY node set for the degenerate
// "no plan authored" case (no planned nodes, no edges), so the live roles are
// never drawn as a flat pseudo-DAG stage (BUG-013).
//
// Sub-coordinator drill-in (Node.Drillable, D6) needs the whole Model to see
// sibling orchestrators, which a lone heramodel.OrchView cannot; this single-arg form (the
// test surface) leaves every node un-drillable. The Hera page calls
// heraPlanNodesWithBridge with the rail's bridge index to stamp Drillable.
func heraPlanNodes(orch *heramodel.OrchView) ([]planview.Node, []planview.Edge) {
	return heraPlanNodesWithBridge(orch, nil)
}

// heraPlanNodesWithBridge is the Model-aware projection: `bridge` maps each
// orchestrator's coordinator bridge task to the orchestrator it coordinates
// (Model.BridgeIndex). A worker node whose bound task is a key in that map IS a
// sub-coordinator (its task is some child orchestrator's coordinator) and is
// marked Drillable. Pass nil bridge to disable drill-in (the lone-heramodel.OrchView path).
func heraPlanNodesWithBridge(orch *heramodel.OrchView, bridge map[string]*heramodel.OrchView) ([]planview.Node, []planview.Edge) {
	if orch == nil {
		return nil, nil
	}
	pnodes, pedges := heraplan.Project(orch, bridge)

	nodeID := make(map[int64]string, len(pnodes))
	nodes := make([]planview.Node, 0, len(pnodes))
	for i := range pnodes {
		pn := &pnodes[i]
		r := pn.Role
		id := planNodeID(r)
		nodeID[pn.RoleID] = id
		state := planNodeState(r)
		nodes = append(nodes, planview.Node{
			ID:             id,
			Name:           pn.Name,
			Planned:        pn.Planned,
			State:          state,
			Description:    pn.Prompt,
			Icon:           planNodeIcon(r, state),
			Archetype:      pn.Archetype,
			Model:          r.AppliedModel,
			Effort:         r.AppliedEffort,
			ProfileWarning: r.ProfileWarning,
			Drillable:      pn.BridgeTarget,
		})
	}

	edges := make([]planview.Edge, 0, len(pedges))
	for _, e := range pedges {
		edges = append(edges, planview.Edge{From: nodeID[e.BlockerRoleID], To: nodeID[e.DependentRoleID]})
	}

	uxlog.Log("[planview] projected orch %d: %d nodes, %d edges", orch.ID, len(nodes), len(edges))
	return nodes, edges
}

// planIsAuthored reports whether a projected plan is an AUTHORED plan: at least
// one planned (never-bound) node OR at least one blocking edge. A degenerate
// projection — only live worker roles, with no planned nodes and no edges — is
// NOT an authored plan: the DAG depicts the plan a coordinator authored, while
// the live roster is the rail's job. The Plan pane renders the empty-plan state
// for the degenerate case (rebuildPlan feeds the widget an empty node set)
// rather than drawing the live roles as a flat pseudo-DAG stage (BUG-013). A
// sub-coordinator bridge with neither planned nodes nor block edges is likewise
// not authored — its drillable live node is not a planned node or an edge.
func planIsAuthored(nodes []planview.Node, edges []planview.Edge) bool {
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

// planNodeID is a plan node's stable identity: a live node's bound argus task id
// (so the planview OnEnter can jump to its agent view) or, for a never-bound
// planned role, a synthetic key derived from the role id. The two id spaces
// cannot collide — a synthetic key is prefixed, a task id never is.
func planNodeID(r *heramodel.RoleView) string {
	if r.TaskID != "" {
		return r.TaskID
	}
	if r.BridgeTaskID != "" {
		// A finished worker (binding ended, not planned) still keys off its task.
		return r.BridgeTaskID
	}
	return fmt.Sprintf("plan:%d", r.RoleID)
}

// planNodeState maps a role to its render state (D7). Cancelled wins over all
// other discriminators — a cancelled planned node renders grey ✕ (StateCancelled)
// regardless of its other fields. A planned (never-bound) role is StatePlanned
// (violet ○). A live/finished role colours from its bound argus task status +
// result, with the {"failed":true} result winning over the workflow status (red ✕)
// — reusing coordTaskFailed (details.go), not a third copy.
func planNodeState(r *heramodel.RoleView) planview.State {
	switch heraplan.StateOf(r) {
	case heraplan.StateCancelled:
		return planview.StateCancelled
	case heraplan.StatePlanned:
		return planview.StatePlanned
	case heraplan.StateFailed:
		return planview.StateFailed
	case heraplan.StateDone:
		return planview.StateDone
	case heraplan.StateInReview:
		return planview.StateInReview
	case heraplan.StateInProgress:
		return planview.StateWorking
	default:
		return planview.StatePending
	}
}

// planNodeIcon resolves a LIVE node's status icon 1:1 with the rail (BUG-007):
// the SAME shared classifier (widget.RoleStatusIcon over roleStatusInputs) the
// rail's statusIcon uses, so the plan node and the rail row never disagree. It
// returns nil for the two plan-view-specific overlays the rail has no concept of
// — a PLANNED (never-bound) node (rendered ○ via State) and a FAILED node
// (rendered ✕ via State) — letting the widget fall back to the State glyph there.
//
// Animated marks the genuinely-active "working" case so the widget re-resolves
// the live spinner frame at Draw. It is derived from the classifier's OWN frame-0
// output (glyph == the spinner frame), NOT from in.Active directly: in.Active is
// NOT mutually exclusive with the higher-precedence signals (a live in_progress
// worker blocked on a prompt is BOTH active AND needs-input), and the classifier
// correctly resolves that to the STATIC needs-input "?" (needs-input outranks
// active). Flagging such a node Animated would make the widget swap the "?" for
// the spinner frame at Draw, breaking 1:1 parity with the rail row (which shows
// "?"). Comparing the resolved glyph to the spinner keeps Animated true ONLY when
// the spinner actually won, with zero duplication of the classifier's precedence
// (and it tracks the active spinner style, which both sides read). See BUG-012.
func planNodeIcon(r *heramodel.RoleView, state planview.State) *planview.NodeIcon {
	if state == planview.StatePlanned || state == planview.StateFailed || state == planview.StateCancelled {
		return nil // ○ / ✕ / cancelled ✕ overlays come from State (the rail has no concept of these)
	}
	in := roleStatusInputs(r)
	glyph, style := widget.RoleStatusIcon(in, false, 0)
	animated := glyph == widget.SpinnerFrame(0)
	return &planview.NodeIcon{Glyph: glyph, Style: style, Animated: animated}
}
