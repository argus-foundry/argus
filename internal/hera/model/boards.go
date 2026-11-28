package model

import "github.com/argus-foundry/argus/internal/db"

// BoardCoordinator is the fully expanded coordinator-only rail projection.
// Counts are attached by the API from one board snapshot, never used to order it.
type BoardCoordinator struct {
	RoleID       int64  `json:"role_id"`
	ParentRoleID *int64 `json:"parent_role_id"`
	Orchestrator string `json:"orchestrator"`
	Coordinator  string `json:"coordinator"`
	Decisions    int    `json:"decisions"`
	FYIs         int    `json:"fyis"`
	Actions      int    `json:"actions"`
	Todos        int    `json:"todos"`
	ReadyActions int    `json:"ready_actions"`
	Deferred     int    `json:"deferred"`
}

// BoardNavigation shares the rail's canonical parent and section ordering.
// Folds are presentation only; archived ancestors and bridge roles deny access
// to the entire branch. Never rescue their children as top-level orphans.
func (m Model) BoardNavigation() []BoardCoordinator {
	out := make([]BoardCoordinator, 0)
	parents := m.CanonicalParents()
	if m.ExplicitLinks != nil {
		parents = make(map[int64]CanonParent, len(m.ExplicitLinks))
		for child, link := range m.ExplicitLinks {
			r := m.RoleByID(link.ParentRoleID)
			parents[child] = CanonParent{OrchID: link.ParentOrchestratorID, CoordSpawn: r != nil && r.Kind == db.HeraKindCoordinator}
		}
	}
	all := make([]*OrchView, 0)
	for _, sec := range [][]OrchView{m.Pinned, m.Active, m.Archived} {
		for i := range sec {
			all = append(all, &sec[i])
		}
	}
	seen := map[int64]bool{}
	// Visibility is inherited even when pinning moves a row's presentation.
	var eligible func(*OrchView, map[int64]bool) bool
	eligible = func(o *OrchView, path map[int64]bool) bool {
		c := o.CoordRole()
		if path[o.ID] || o.Archived || c == nil || c.Archived {
			return false
		}
		cp, nested := parents[o.ID]
		if !nested {
			return true
		}
		parent := m.OrchByID(cp.OrchID)
		if parent == nil {
			return false
		}
		_, roleID, ok := m.BridgeParentOf(o.ID)
		if link, found := m.ExplicitLinks[o.ID]; found {
			roleID, ok = link.ParentRoleID, true
		}
		role := m.RoleByID(roleID)
		if !ok || role == nil || role.Archived {
			return false
		}
		path[o.ID] = true
		return eligible(parent, path)
	}
	floated := map[int64]bool{}
	var floatedOrder []*OrchView
	for _, o := range all {
		if o.Pinned || o.Archived {
			continue
		}
		for _, r := range o.Roles {
			if !r.Pinned || r.Archived || r.Kind == db.HeraKindCoordinator {
				continue
			}
			for _, child := range all {
				_, bridge, ok := m.BridgeParentOf(child.ID)
				if link, found := m.ExplicitLinks[child.ID]; found {
					bridge, ok = link.ParentRoleID, true
				}
				if cp, found := parents[child.ID]; found && !cp.CoordSpawn && cp.OrchID == o.ID && ok && bridge == r.RoleID {
					if !floated[child.ID] {
						floatedOrder = append(floatedOrder, child)
					}
					floated[child.ID] = true
				}
			}
		}
	}
	var visit func(*OrchView, *int64)
	visit = func(o *OrchView, parent *int64) {
		if seen[o.ID] {
			return
		}
		seen[o.ID] = true
		c := o.CoordRole()
		if !eligible(o, map[int64]bool{}) {
			return
		}
		out = append(out, BoardCoordinator{RoleID: c.RoleID, ParentRoleID: parent, Orchestrator: o.Name, Coordinator: c.Name})
		// Workers supply the nested coordinator position but never a board row.
		for _, r := range o.Roles {
			if r.Kind == db.HeraKindCoordinator || r.Archived {
				continue
			}
			for _, child := range all {
				cp, ok := parents[child.ID]
				if !ok || cp.OrchID != o.ID || cp.CoordSpawn || floated[child.ID] {
					continue
				}
				_, bridge, ok := m.BridgeParentOf(child.ID)
				if link, found := m.ExplicitLinks[child.ID]; found {
					bridge = link.ParentRoleID
					ok = true
				}
				if ok && bridge == r.RoleID {
					visit(child, &c.RoleID)
				}
			}
		}
		for _, child := range all {
			if cp, ok := parents[child.ID]; ok && cp.OrchID == o.ID && cp.CoordSpawn {
				visit(child, &c.RoleID)
			}
		}
	}
	for i := range m.Pinned {
		visit(&m.Pinned[i], nil)
	}
	for _, child := range floatedOrder {
		visit(child, nil)
	}
	for _, group := range RailKanbanOrder() {
		for i := range m.Active {
			o := &m.Active[i]
			status := o.KanbanStatus
			if status == "" {
				status = db.HeraKanbanActive
			}
			if _, child := parents[o.ID]; !child && status == group {
				visit(o, nil)
			}
		}
	}
	return out
}

// RailKanbanOrder is shared by navigation and the TUI rail's root traversal.
func RailKanbanOrder() []db.HeraKanbanStatus {
	return []db.HeraKanbanStatus{db.HeraKanbanActive, db.HeraKanbanBacklog, db.HeraKanbanBlocked, db.HeraKanbanDone}
}
