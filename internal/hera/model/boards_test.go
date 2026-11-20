package model

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardNavigationRailOrderAndVisibility(t *testing.T) {
	coord := func(id int64) RoleView { return RoleView{RoleID: id, Kind: db.HeraKindCoordinator} }
	m := Model{Pinned: []OrchView{{ID: 1, Name: "pin", Roles: []RoleView{coord(10)}}}, Active: []OrchView{
		{ID: 2, Name: "backlog", KanbanStatus: db.HeraKanbanBacklog, Roles: []RoleView{coord(20)}},
		{ID: 3, Name: "active", KanbanStatus: db.HeraKanbanActive, Roles: []RoleView{coord(30), {RoleID: 31, Kind: db.HeraKindWorker}}},
		{ID: 4, Name: "child", Roles: []RoleView{coord(40)}},
	}, Archived: []OrchView{{ID: 5, Archived: true, Roles: []RoleView{coord(50)}}}, ExplicitLinks: map[int64]db.HeraOrchLink{4: {ParentOrchestratorID: 3, ChildOrchestratorID: 4, ParentRoleID: 31}}}
	rows := m.BoardNavigation()
	ids := []int64{}
	for _, row := range rows {
		ids = append(ids, row.RoleID)
	}
	testutil.DeepEqual(t, ids, []int64{10, 30, 40, 20})
	testutil.Equal(t, *rows[2].ParentRoleID, int64(30))
	testutil.Nil(t, rows[0].ParentRoleID)
	m.Active[1].Roles[1].Pinned = true
	rows = m.BoardNavigation()
	testutil.Equal(t, rows[1].RoleID, int64(40))
	testutil.Nil(t, rows[1].ParentRoleID)
	m.Active[1].Roles[1].Pinned = false
	m.Active[1].Roles[1].Archived = true
	rows = m.BoardNavigation()
	testutil.Equal(t, len(rows), 3)
	m.Active[1].Roles[0].Archived = true
	rows = m.BoardNavigation()
	testutil.Equal(t, len(rows), 2)
}

func TestBoardNavigationCoordinatorLinksAndCycles(t *testing.T) {
	c := func(id int64) RoleView { return RoleView{RoleID: id, Kind: db.HeraKindCoordinator} }
	m := Model{Active: []OrchView{{ID: 1, Roles: []RoleView{c(10)}}, {ID: 2, Roles: []RoleView{c(20)}}, {ID: 3}}, ExplicitLinks: map[int64]db.HeraOrchLink{2: {ParentOrchestratorID: 1, ChildOrchestratorID: 2, ParentRoleID: 10}}}
	rows := m.BoardNavigation()
	testutil.Equal(t, len(rows), 2)
	testutil.Equal(t, *rows[1].ParentRoleID, int64(10))
	m.Active[0].Archived = true
	testutil.Equal(t, len(m.BoardNavigation()), 0)
	m.Active[0].Archived = false
	m.ExplicitLinks[1] = db.HeraOrchLink{ParentOrchestratorID: 2, ChildOrchestratorID: 1, ParentRoleID: 20}
	testutil.Equal(t, len(m.BoardNavigation()), 0)
	m.ExplicitLinks = nil
	rows = m.BoardNavigation()
	testutil.Equal(t, len(rows), 2)
}

func TestBoardNavigationPinnedBridgesFollowParentRoleOrder(t *testing.T) {
	m := Model{Active: []OrchView{
		{ID: 1, Roles: []RoleView{{RoleID: 10, Kind: db.HeraKindCoordinator}, {RoleID: 11, Kind: db.HeraKindWorker, Pinned: true}, {RoleID: 12, Kind: db.HeraKindWorker, Pinned: true}}},
		{ID: 2, Roles: []RoleView{{RoleID: 20, Kind: db.HeraKindCoordinator}}},
		{ID: 3, Roles: []RoleView{{RoleID: 30, Kind: db.HeraKindCoordinator}}},
	}, ExplicitLinks: map[int64]db.HeraOrchLink{2: {ParentOrchestratorID: 1, ParentRoleID: 12}, 3: {ParentOrchestratorID: 1, ParentRoleID: 11}}}
	rows := m.BoardNavigation()
	var ids []int64
	for _, row := range rows {
		ids = append(ids, row.RoleID)
	}
	testutil.DeepEqual(t, ids, []int64{30, 20, 10})
	m.Active[0].Roles[0].Archived = true
	testutil.Equal(t, len(m.BoardNavigation()), 0)
}
