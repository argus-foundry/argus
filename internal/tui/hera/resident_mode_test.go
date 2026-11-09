package hera

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

// residentFixture builds the standing team (resident coordinator + two
// workers) plus an ordinary project orchestrator, and returns a page in
// resident mode with the cursor on the resident row.
type residentFixture struct {
	d       *db.DB
	p       *HeraPage
	orchID  int64
	workers []string
}

func newResidentFixture(t *testing.T) *residentFixture {
	t.Helper()
	d := memDB(t)
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	f := &residentFixture{d: d, orchID: o.ID}
	for _, name := range []string{"alpha", "beta"} {
		id := "task-" + name
		testutil.NoError(t, d.Add(&model.Task{ID: id, Name: name, Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
		_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "p"}, id, "/wt/"+id)
		testutil.NoError(t, err)
		testutil.NoError(t, d.SetMeta(id, "hera", db.HeraMetaKeyResidentWorker, "true"))
		f.workers = append(f.workers, id)
	}
	// An ordinary project must never appear in resident mode.
	proj := seedOrch(t, d, "proj")
	seedBoundRole(t, d, proj, "pc", db.HeraKindCoordinator, "task-proj-c")
	p := NewHeraPage(d)
	p.SetResidentReader(heramodel.NewResidentReader(d, d))
	f.p = p
	return f
}

func (f *residentFixture) enter(t *testing.T) {
	t.Helper()
	f.p.SetResidentMode(true)
	testutil.True(t, f.p.ResidentMode())
}

func (f *residentFixture) selectTask(t *testing.T, id string) {
	t.Helper()
	testutil.True(t, f.p.JumpToTask(id))
	f.p.Machine().ToRail() // JumpToTask parks focus in the pane; keys under test are rail keys
}

type residentCalls struct{ n map[string]int }

func (f *residentFixture) wire() *residentCalls {
	c := &residentCalls{n: map[string]int{}}
	rec := func(name string) func(heramodel.Selection) {
		return func(heramodel.Selection) { c.n[name]++ }
	}
	f.p.OnDelete = rec("delete")
	f.p.OnRename = rec("rename")
	f.p.OnArchiveToggle = rec("archive")
	f.p.OnStatusAdvance = rec("stat+")
	f.p.OnStatusRevert = rec("stat-")
	f.p.OnSpawnWorker = rec("spawn")
	f.p.OnPinToggle = rec("pin")
	f.p.OnKanbanAdvance = rec("kanban+")
	f.p.OnKanbanRevert = rec("kanban-")
	f.p.OnAdopt = rec("adopt")
	f.p.OnClearArchive = rec("clear")
	f.p.OnNewCoordinator = rec("newcoord")
	f.p.OnCleanup = rec("cleanup")
	f.p.OnForceRecycle = rec("recycle")
	f.p.OnBounceWorker = rec("bounce")
	f.p.OnReattach = rec("reattach")
	f.p.OnInbox = func(string) { c.n["inbox"]++ }
	f.p.OnMoveAccount = func(string) { c.n["move"]++ }
	return c
}

func key(r rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone) }

func TestResidentMode_ModelIsOnlyTheStandingTeam(t *testing.T) {
	f := newResidentFixture(t)
	f.p.Refresh()
	generic := f.p.Rail().Model()
	testutil.Equal(t, len(generic.Active), 1)
	testutil.Equal(t, generic.Active[0].Name, "proj")

	f.enter(t)
	m := f.p.Rail().Model()
	testutil.Equal(t, len(m.Active), 1)
	testutil.Equal(t, m.Active[0].ID, f.orchID)
	testutil.Equal(t, len(m.Pinned)+len(m.Archived), 0)

	f.p.SetResidentMode(false)
	testutil.False(t, f.p.ResidentMode())
	m = f.p.Rail().Model()
	testutil.Equal(t, len(m.Active), 1)
	testutil.Equal(t, m.Active[0].Name, "proj")
}

func TestResidentMode_PanesFollowRow(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	residentTask, err := f.d.ResidentTask()
	testutil.NoError(t, err)

	// Resident row: the resident session is the conversation pane.
	testutil.Equal(t, f.p.CoordPane().TaskID(), residentTask.ID)

	// Worker row: the worker shows in the worker pane; the resident stays put.
	f.selectTask(t, f.workers[0])
	testutil.Equal(t, f.p.AgentPane().TaskID(), f.workers[0])
	testutil.Equal(t, f.p.CoordPane().TaskID(), residentTask.ID)
}

func TestResidentMode_InertKeysFireNothing(t *testing.T) {
	inert := []*tcell.EventKey{
		key('w'), key('n'), key('P'), key('m'), key('M'), key('J'), key('c'), key('C'), key('>'), key('B'),
	}
	for _, onWorker := range []bool{false, true} {
		name := "resident row"
		if onWorker {
			name = "worker row"
		}
		t.Run(name, func(t *testing.T) {
			f := newResidentFixture(t)
			f.enter(t)
			calls := f.wire()
			if onWorker {
				f.selectTask(t, f.workers[0])
			}
			clear(calls.n) // navigation itself may reattach; only the keys under test count
			h := f.p.InputHandler()
			for _, ev := range inert {
				h(ev, noFocus)
			}
			testutil.Equal(t, len(calls.n), 0)
		})
	}
}

func TestResidentMode_AllowedKeysActOnWorkerRowsOnly(t *testing.T) {
	allowed := map[string]*tcell.EventKey{
		"delete":  tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModCtrl),
		"rename":  key('r'),
		"archive": key('a'),
		"stat+":   key('s'),
		"stat-":   key('S'),
	}
	t.Run("worker row", func(t *testing.T) {
		f := newResidentFixture(t)
		f.enter(t)
		calls := f.wire()
		f.selectTask(t, f.workers[0])
		h := f.p.InputHandler()
		for name, ev := range allowed {
			h(ev, noFocus)
			testutil.Equal(t, calls.n[name], 1)
		}
		h(key('i'), noFocus)
		testutil.Equal(t, calls.n["inbox"], 1)
	})
	t.Run("resident row is protected", func(t *testing.T) {
		f := newResidentFixture(t)
		f.enter(t)
		calls := f.wire()
		h := f.p.InputHandler()
		for _, ev := range allowed {
			h(ev, noFocus)
		}
		testutil.Equal(t, len(calls.n), 0)
		// The inbox is the resident's own and works from its row.
		h(key('i'), noFocus)
		testutil.Equal(t, calls.n["inbox"], 1)
	})
}

func TestResidentMode_KeysUnchangedOutsideResidentMode(t *testing.T) {
	f := newResidentFixture(t)
	calls := f.wire()
	f.p.Refresh()
	h := f.p.InputHandler()
	h(key('n'), noFocus) // new coordinator is live in the Projects tab
	testutil.Equal(t, calls.n["newcoord"], 1)
}

func TestResidentMode_ResidentRowEnterDoesNotReattach(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	calls := f.wire()
	h := f.p.InputHandler()
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noFocus)
	testutil.Equal(t, calls.n["reattach"], 0) // the daemon owns the resident session
	f.p.SetResidentMode(false)
	f.p.SetResidentMode(true)
	f.selectTask(t, f.workers[0])
	calls.n["reattach"] = 0
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noFocus)
	testutil.Equal(t, calls.n["reattach"], 1) // workers revive as usual
}

func TestResidentMode_HideKeepsWorkerOnRail(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	role := roleForTask(t, f.d, f.orchID, f.workers[0])
	testutil.NoError(t, f.d.ArchiveHeraRole(role.ID))
	f.p.Refresh()
	yes, err := f.d.IsResidentWorker(f.workers[0])
	testutil.NoError(t, err)
	testutil.True(t, yes)
	// Still part of the resident view (dimmed in place) after hide...
	found := false
	annotate(f.p, func(rv *heramodel.RoleView) {
		if rv.TaskID == f.workers[0] {
			found = true
			testutil.True(t, rv.Archived)
		}
	})
	testutil.True(t, found)
	// ...and un-hide restores it as an ordinary live row.
	testutil.NoError(t, f.d.UnarchiveHeraRole(role.ID))
	f.p.Refresh()
	annotate(f.p, func(rv *heramodel.RoleView) {
		if rv.TaskID == f.workers[0] {
			testutil.False(t, rv.Archived)
		}
	})
}

func TestResidentMode_NoSizeDriftKickOnResidentPane(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	testutil.Equal(t, f.p.coordKickBound(), "")
	f.p.SetResidentMode(false)
	testutil.Equal(t, f.p.coordKickBound(), f.p.coordBound)
}

func roleForTask(t *testing.T, d *db.DB, orchID int64, taskID string) *db.HeraRole {
	t.Helper()
	roles, err := d.ListHeraRoles(orchID, true)
	testutil.NoError(t, err)
	binds, err := d.ListHeraLiveBindings()
	testutil.NoError(t, err)
	for _, b := range binds {
		if b.ArgusTaskID == taskID {
			for _, r := range roles {
				if r.ID == b.RoleID {
					return r
				}
			}
		}
	}
	t.Fatalf("no role for task %s", taskID)
	return nil
}

func annotate(p *HeraPage, fn func(*heramodel.RoleView)) {
	m := p.Rail().Model()
	m.AnnotateRoles(fn)
}
