package plan

import (
	"testing"

	"github.com/drn/argus/internal/db"
	heramodel "github.com/drn/argus/internal/hera/model"
	"github.com/drn/argus/internal/testutil"
)

func TestStateOf(t *testing.T) {
	tests := []struct {
		name string
		r    heramodel.RoleView
		want State
	}{
		{"cancelled wins", heramodel.RoleView{Cancelled: true, Planned: true, TaskStatus: "complete"}, StateCancelled},
		{"planned", heramodel.RoleView{Planned: true}, StatePlanned},
		{"failed result beats status", heramodel.RoleView{TaskStatus: "complete", TaskResult: `{"failed":true}`}, StateFailed},
		{"bad result json ignored", heramodel.RoleView{TaskStatus: "complete", TaskResult: `{`}, StateDone},
		{"in review", heramodel.RoleView{TaskStatus: "in_review"}, StateInReview},
		{"in progress", heramodel.RoleView{TaskStatus: "in_progress"}, StateInProgress},
		{"pending default", heramodel.RoleView{TaskStatus: "weird"}, StatePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, StateOf(&tt.r), tt.want)
		})
	}
}

func TestProjectFiltersAndBridge(t *testing.T) {
	orch := &heramodel.OrchView{ID: 1, Roles: []heramodel.RoleView{
		{RoleID: 1, Kind: db.HeraKindCoordinator},
		{RoleID: 2, Kind: db.HeraKindWorker, Planned: true},
		{RoleID: 3, Kind: db.HeraKindWorker, BridgeTaskID: "t3", TaskStatus: "in_progress"},
		{RoleID: 4, Kind: db.HeraKindWorker, Archived: true},
	}, Blocks: []db.HeraBlock{
		{BlockerRoleID: 2, BlockedRoleID: 3},
		{BlockerRoleID: 2, BlockedRoleID: 4},
		{BlockerRoleID: 1, BlockedRoleID: 3},
	}}
	child := &heramodel.OrchView{ID: 9}
	nodes, edges := Project(orch, map[string]*heramodel.OrchView{"t3": child, "t2": orch})
	testutil.Equal(t, len(nodes), 2)
	testutil.Equal(t, nodes[1].TaskID, "t3")
	testutil.Equal(t, nodes[1].ChildOrchID, int64(9))
	testutil.Equal(t, nodes[0].ChildOrchID, int64(0))
	testutil.Equal(t, len(edges), 1)
	testutil.True(t, IsAuthored(nodes, edges))
	testutil.Equal(t, WireID(7), "role:7")

	n2, e2 := Project(nil, nil)
	testutil.Equal(t, len(n2)+len(e2), 0)
	live := []Node{{RoleID: 3}}
	testutil.Equal(t, IsAuthored(live, nil), false)
}
