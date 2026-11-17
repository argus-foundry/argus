package hera

import (
	"slices"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Pending (optimistic) coordinator rows — instant-launch-new-coordinator.
//
// A new root coordinator's create (worktree + session) takes seconds and has no
// DB identity until it finishes, so the rail would show nothing meanwhile. The
// page keeps a small registry of in-flight creates and overlays one synthetic
// orchestrator (+ one coordinator role) per entry onto every model it installs
// on the rail. The overlay is UI-goroutine-only and never touches the store,
// the pristine model used for the equality drop, or the data-version gate.

// pendingCoord is one in-flight root-coordinator create.
type pendingCoord struct {
	id   int64 // synthetic orchestrator ID (>= heramodel.PendingIDBase)
	name string
	proj string
}

// withPending returns m with one synthetic Active orchestrator per pending
// create appended. m itself is not mutated (the Active slice is cloned before
// the append), so the caller's pristine/equality state is unaffected.
func (p *HeraPage) withPending(m heramodel.Model) heramodel.Model {
	if len(p.pendingCoords) == 0 {
		return m
	}
	active := slices.Clone(m.Active)
	for _, pc := range p.pendingCoords {
		active = append(active, heramodel.OrchView{
			ID:           pc.id,
			Name:         pc.name,
			Pending:      true,
			KanbanStatus: db.HeraKanbanActive,
			Roles: []heramodel.RoleView{{
				RoleID:       pc.id,
				OrchID:       pc.id,
				Name:         "coord",
				Kind:         db.HeraKindCoordinator,
				ArgusProject: pc.proj,
				Pending:      true,
			}},
		})
	}
	m.Active = active
	return m
}

// AddPendingCoordinator registers an in-flight root-coordinator create and
// shows it immediately: the pending row is overlaid on the rail, selected, and
// focus moves to the coordinator pane, which shows the shared LAUNCH banner
// (applySelection drives SetPending from the selected pending row). Returns the
// synthetic orchestrator ID to hand back to ResolvePendingCoordinator. MUST run
// on the tview goroutine. A no-op (0) in remote mode, where the rail is inert.
func (p *HeraPage) AddPendingCoordinator(name, project string) int64 {
	if p.remote {
		return 0
	}
	p.pendingSeq++
	id := heramodel.PendingIDBase + p.pendingSeq
	p.pendingCoords = append(p.pendingCoords, pendingCoord{id: id, name: name, proj: project})
	p.Refresh()
	if p.rail.SelectOrch(id) {
		p.focus.SetRegion(FocusCoord)
	}
	uxlog.Log("[hera-view] pending coordinator added: id=%d name=%s project=%s selected=%v",
		id, name, project, p.pendingSelected(id))
	return id
}

// pendingSelected reports whether the rail cursor is on pending row id.
func (p *HeraPage) pendingSelected(id int64) bool {
	o := p.rail.Selection().Orch
	return o != nil && o.ID == id
}

// ResolvePendingCoordinator ends a pending create. On success the real
// orchestrator (realOrchID) replaces the pending row; when the pending row was
// still the selected row, selection and focus land on the real coordinator
// (already live). When the operator navigated elsewhere meanwhile, the rail
// refreshes and neither selection nor focus moves. On failure (realOrchID 0)
// the pending row is simply removed. MUST run on the tview goroutine.
func (p *HeraPage) ResolvePendingCoordinator(id, realOrchID int64) {
	idx := slices.IndexFunc(p.pendingCoords, func(pc pendingCoord) bool { return pc.id == id })
	if idx < 0 {
		return
	}
	wasSelected := p.pendingSelected(id)
	p.pendingCoords = slices.Delete(p.pendingCoords, idx, idx+1)
	p.Refresh()
	landed := false
	if realOrchID != 0 && wasSelected && p.rail.SelectOrch(realOrchID) {
		// Focus only — never landOnSelection, whose reattach branch could
		// restart a session that CreateAndStart just started.
		p.focus.SetRegion(FocusCoord)
		landed = true
	}
	uxlog.Log("[hera-view] pending coordinator resolved: id=%d real=%d wasSelected=%v landed=%v",
		id, realOrchID, wasSelected, landed)
}

// PendingCoordinators reports how many creates are in flight (tests).
func (p *HeraPage) PendingCoordinators() int { return len(p.pendingCoords) }

// pendingInert surfaces why a mutation key did nothing on a pending row.
func (p *HeraPage) pendingInert() {
	uxlog.Log("[hera-view] mutation ignored: selected coordinator is still launching")
	if p.OnInfo != nil {
		p.OnInfo("Coordinator is still launching")
	}
}
