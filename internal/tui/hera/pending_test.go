package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWithPending_DoesNotMutateInputAndUsesHugeIDs(t *testing.T) {
	p := NewHeraPage(openFileDB(t))
	p.pendingCoords = []pendingCoord{{id: heramodel.PendingIDBase + 1, name: "n", proj: "p"}}
	base := heramodel.Model{Active: make([]heramodel.OrchView, 1, 4)}
	base.Active[0].Name = "real"

	got := p.withPending(base)
	testutil.Equal(t, len(base.Active), 1)
	testutil.Equal(t, len(got.Active), 2)
	// The append must not have written into base's spare capacity.
	testutil.Equal(t, base.Active[:2][1].Name, "")
	pend := got.Active[1]
	testutil.Equal(t, pend.Pending, true)
	testutil.Equal(t, pend.ID >= heramodel.PendingIDBase, true)
	testutil.Equal(t, pend.CoordRole() != nil && pend.CoordRole().Pending, true)
	testutil.Equal(t, pend.CoordTaskID(), "") // no task: coordinator pane stays unbound
}

// An async tick publish (a different model install than Refresh) must keep the
// pending row, and the real model's pristine/equality state must not see it.
func TestPendingRow_SurvivesAsyncPublish(t *testing.T) {
	p, d, q := asyncPage(t)
	seedTeam(t, d, "real")
	p.Refresh()

	id := p.AddPendingCoordinator("fresh", "proj")
	testutil.Equal(t, id >= heramodel.PendingIDBase, true)
	testutil.Equal(t, p.pendingSelected(id), true)
	testutil.Equal(t, p.focus.State(), FocusCoord)
	testutil.Equal(t, p.coordPane.Pending(), true)

	seedTeam(t, d, "second") // a real change so the next tick publishes
	p.InvalidateChangeGate()
	p.ScheduleRefresh()
	p.refresher.Flush()
	q.pump(t)

	names := railOrchNames(p)
	testutil.Equal(t, len(names), 3)
	testutil.Equal(t, p.pendingSelected(id), true)
	for _, o := range p.lastPristine.Active {
		testutil.Equal(t, o.Pending, false)
	}

	p.ResolvePendingCoordinator(id, 0)
	testutil.Equal(t, p.PendingCoordinators(), 0)
	testutil.Equal(t, len(railOrchNames(p)), 2)
	testutil.Equal(t, p.coordPane.Pending(), false)
}

func TestPendingRow_MutationsInert(t *testing.T) {
	p := NewHeraPage(openFileDB(t))
	var infos []string
	p.OnInfo = func(s string) { infos = append(infos, s) }
	called := false
	p.OnDelete = func(heramodel.Selection) { called = true }
	p.AddPendingCoordinator("fresh", "proj")

	sel := p.rail.Selection()
	testutil.Equal(t, sel.IsPending(), true)
	testutil.Equal(t, p.fire(p.OnDelete, sel), true)
	testutil.Equal(t, called, false)
	testutil.Equal(t, len(infos), 1)
	testutil.Equal(t, sel.Orch.CoordRole().Kind, db.HeraKindCoordinator)
}

func TestAddPendingCoordinator_RemoteIsNoop(t *testing.T) {
	p := NewHeraPage(nil)
	p.remote = true
	testutil.Equal(t, p.AddPendingCoordinator("x", "p"), int64(0))
	testutil.Equal(t, p.PendingCoordinators(), 0)
	p.ResolvePendingCoordinator(123, 0) // unknown id: no panic
}
