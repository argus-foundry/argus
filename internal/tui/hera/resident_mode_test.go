package hera

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestResidentRailDisplayName(t *testing.T) {
	for _, tt := range []struct{ name, display, want string }{
		{"custom", "Solo", "Solo"},
		{"unicode", "Sólo", "Sólo"},
		{"empty", "", "resident"},
		{"control", "So\x1blo", "resident"},
		{"too long", strings.Repeat("x", config.ResidentNameMaxRunes+1), "resident"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newResidentFixture(t)
			f.enter(t)
			f.p.SetResidentTitle(tt.display)
			text := joinRows(railScreen(t, f.p.Rail(), 40, 12))
			testutil.Contains(t, text, tt.want)
			testutil.Contains(t, text, "alpha")
			testutil.Contains(t, text, "beta")
			m := f.p.Rail().Model()
			testutil.Equal(t, m.Active[0].Name, "resident")
			testutil.Equal(t, m.Active[0].CoordRole().Name, "resident")
			// A refresh must preserve the label without rewriting the model.
			f.p.Reconcile()
			testutil.Contains(t, joinRows(railScreen(t, f.p.Rail(), 40, 12)), tt.want)
			f.p.SetResidentMode(false)
			// Even a resident-named coordinator outside this tab keeps its identity label.
			f.p.Rail().SetModel(m)
			text = joinRows(railScreen(t, f.p.Rail(), 40, 12))
			testutil.Contains(t, text, "resident")
			if tt.want != "resident" {
				testutil.False(t, strings.Contains(text, tt.want))
			}
		})
	}
}

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
		_, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "p"}, id, "/wt/"+id)
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

type residentCalls struct {
	n        map[string]int
	moveTask string
}

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
	f.p.OnMoveAccount = func(id string) { c.n["move"]++; c.moveTask = id }
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
	// `>` is the resident's own account move: live on its row, inert on workers.
	withoutMove := func(evs []*tcell.EventKey) (out []*tcell.EventKey) {
		for _, ev := range evs {
			if ev.Rune() != '>' {
				out = append(out, ev)
			}
		}
		return out
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
			keys := inert
			if !onWorker {
				keys = withoutMove(inert)
			}
			for _, ev := range keys {
				h(ev, noFocus)
			}
			testutil.Equal(t, len(calls.n), 0)
		})
	}
}

func TestResidentMode_MoveAccountOnlyOnResidentRow(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	calls := f.wire()
	residentTask, err := f.d.ResidentTask()
	testutil.NoError(t, err)

	h := f.p.InputHandler()
	h(key('>'), noFocus)
	testutil.Equal(t, calls.n["move"], 1)
	testutil.Equal(t, calls.moveTask, residentTask.ID)

	f.selectTask(t, f.workers[0])
	clear(calls.n)
	h(key('>'), noFocus)
	testutil.Equal(t, calls.n["move"], 0)
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

// railScreen draws the rail to a sim screen and returns every row as text.
func railScreen(t *testing.T, r *Rail, w, h int) []string {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, sim.Init())
	t.Cleanup(sim.Fini)
	sim.SetSize(w, h)
	r.SetRect(0, 0, w, h)
	r.Draw(sim)
	sim.Show()
	cells, cw, _ := sim.GetContents()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var b []rune
		for x := 0; x < w; x++ {
			if rs := cells[y*cw+x].Runes; len(rs) > 0 {
				b = append(b, rs[0])
			}
		}
		rows[y] = string(b)
	}
	return rows
}

func joinRows(rows []string) string {
	out := ""
	for _, r := range rows {
		out += r + "\n"
	}
	return out
}

func TestRail_UnreadTagOnWorkerRows(t *testing.T) {
	r := NewRail()
	r.SetModel(heramodel.Model{Active: []heramodel.OrchView{{ID: 1, Name: "resident", Roles: []heramodel.RoleView{
		{RoleID: 1, OrchID: 1, Name: "resident", Kind: db.HeraKindCoordinator, Live: true, TaskID: "tc"},
		{RoleID: 2, OrchID: 1, Name: "alpha", Kind: db.HeraKindWorker, Live: true, TaskID: "ta"},
		{RoleID: 3, OrchID: 1, Name: "beta", Kind: db.HeraKindWorker, Live: true, TaskID: "tb"},
	}}}})
	r.SetUnread(map[string]int{"ta": 3})
	text := joinRows(railScreen(t, r, 40, 12))
	testutil.Contains(t, text, "alpha")
	testutil.Contains(t, text, "✉3")
	// Rows with nothing unread stay clean.
	for _, line := range railScreenLines(text) {
		if len(line) > 0 && containsAll(line, "beta") && containsAll(line, "✉") {
			t.Fatalf("beta row shows an unread tag: %q", line)
		}
	}
	// Clearing removes the tag.
	r.SetUnread(nil)
	testutil.False(t, containsAll(joinRows(railScreen(t, r, 40, 12)), "✉"))
}

func railScreenLines(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	return out
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestResidentMode_UnreadCountsReachRailAndPaneTitle(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	f.p.SetResidentTitle("Solo")
	testutil.Equal(t, f.p.coordTitle, "Solo")
	residentTask, err := f.d.ResidentTask()
	testutil.NoError(t, err)
	f.p.SetUnread(map[string]int{residentTask.ID: 2, f.workers[0]: 1})
	testutil.Equal(t, f.p.coordTitle, "Solo · ✉2")
	f.p.SetUnread(nil)
	testutil.Equal(t, f.p.coordTitle, "Solo")
}

type emptyRailStore struct{}

func (emptyRailStore) LoadRailState() (string, error) { return "", nil }
func (emptyRailStore) SaveRailState(string) error     { return nil }

// A first-run rail seeds "collapse everything"; the resident view must always
// show its team, and must not consume that one-shot meant for the Projects view.
func TestResidentMode_TeamAlwaysExpandedAndFirstRunCollapseKept(t *testing.T) {
	f := newResidentFixture(t)
	f.p.SetRailStateStore(emptyRailStore{})
	f.enter(t)
	names := ""
	annotate(f.p, func(rv *heramodel.RoleView) { names += rv.Name + " " })
	rows := joinRows(railScreen(t, f.p.Rail(), 40, 12))
	testutil.Contains(t, rows, "alpha")
	testutil.Contains(t, rows, "beta")
	testutil.True(t, f.p.Rail().firstRunCollapse) // untouched for the Projects view
	f.p.SetResidentMode(false)
	rows = joinRows(railScreen(t, f.p.Rail(), 40, 12))
	testutil.False(t, containsAll(rows, "alpha")) // Projects rail honours its own first-run collapse
}
