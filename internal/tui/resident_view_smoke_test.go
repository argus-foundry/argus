package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
)

// residentViewFixture is the resident tab with a two-worker team on a wide
// simulated terminal, opened and parked on the rail.
type residentViewFixture struct {
	a       *App
	d       *db.DB
	sim     tcell.SimulationScreen
	stop    func()
	workers []string
	roles   []int64
}

func newResidentViewFixture(t *testing.T) *residentViewFixture {
	t.Helper()
	a, d := residentTestApp(t)
	orchs, err := d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	f := &residentViewFixture{a: a, d: d}
	for _, name := range []string{"alpha", "beta"} {
		id := "rw-" + name
		testutil.NoError(t, d.Add(&model.Task{ID: id, Name: name, Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
		role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orchs[0].ID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "p"}, id, t.TempDir())
		testutil.NoError(t, err)
		f.workers = append(f.workers, id)
		f.roles = append(f.roles, role.ID)
	}
	f.sim, f.stop = wireApp(t, a)
	f.sim.SetSize(200, 40)
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.heraPage.Machine().ToRail()
	})
	redrawUI(t, a)
	return f
}

func (f *residentViewFixture) selectWorker(t *testing.T, i int) {
	t.Helper()
	readUI(t, f.a.tapp, func() {
		testutil.True(t, f.a.heraPage.JumpToTask(f.workers[i]))
		f.a.heraPage.Machine().ToRail()
	})
}

func (f *residentViewFixture) selectResident(t *testing.T) {
	t.Helper()
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.Rail().CursorUp()
		f.a.heraPage.Rail().CursorUp()
		f.a.heraPage.Rail().CursorUp()
		f.a.heraPage.Machine().ToRail()
		testutil.True(t, f.a.heraPage.Rail().Selection().IsCoordinator())
	})
}

func (f *residentViewFixture) press(t *testing.T, ev *tcell.EventKey) {
	t.Helper()
	f.a.tapp.QueueEvent(ev)
	syncUI(t, f.a.tapp)
}

func rune1(r rune) *tcell.EventKey { return tcell.NewEventKey(tcell.KeyRune, r, 0) }

func (f *residentViewFixture) counts(t *testing.T) (tasks, orchs int) {
	t.Helper()
	ts, err := f.d.Tasks()
	testutil.NoError(t, err)
	os, err := f.d.ListHeraOrchestrators(true)
	testutil.NoError(t, err)
	return len(ts), len(os)
}

func TestSmoke_ResidentView_CreationKeysInert(t *testing.T) {
	for _, onWorker := range []bool{false, true} {
		name := "resident row"
		if onWorker {
			name = "worker row"
		}
		t.Run(name, func(t *testing.T) {
			f := newResidentViewFixture(t)
			defer f.stop()
			if onWorker {
				f.selectWorker(t, 0)
			} else {
				f.selectResident(t)
			}
			tasks0, orchs0 := f.counts(t)
			// spawn, new coordinator, pin, kanban ±, adopt, clear archive, cleanup, move account, bounce
			for _, r := range []rune{'w', 'n', 'P', 'm', 'M', 'J', 'c', 'C', '>', 'B'} {
				f.press(t, rune1(r))
				readUI(t, f.a.tapp, func() {
					testutil.Equal(t, f.a.mode, modeTaskList)
					testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
					testutil.Equal(t, f.a.tapp.GetFocus() == f.a.heraPage, true)
				})
			}
			tasks1, orchs1 := f.counts(t)
			testutil.Equal(t, tasks1, tasks0)
			testutil.Equal(t, orchs1, orchs0)
		})
	}
}

func TestSmoke_ResidentView_AllowedKeysActOnWorkerRowsOnly(t *testing.T) {
	t.Run("resident row is protected", func(t *testing.T) {
		f := newResidentViewFixture(t)
		defer f.stop()
		f.selectResident(t)
		before, _ := f.d.HeraRoleStatusFor(f.roles[0]) // may be "not found" before any step
		for _, ev := range []*tcell.EventKey{rune1('r'), rune1('a'), rune1('s'), rune1('S'), tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModCtrl)} {
			f.press(t, ev)
			readUI(t, f.a.tapp, func() { testutil.Equal(t, f.a.mode, modeTaskList) })
		}
		after, _ := f.d.HeraRoleStatusFor(f.roles[0])
		testutil.DeepEqual(t, after, before)
		// Every worker is untouched.
		for _, id := range f.roles {
			r, err := f.d.HeraRole(id)
			testutil.NoError(t, err)
			testutil.True(t, r.NukedAt == nil && r.ArchivedAt == nil)
		}
	})
	t.Run("worker row: rename opens the prompt", func(t *testing.T) {
		f := newResidentViewFixture(t)
		defer f.stop()
		f.selectWorker(t, 0)
		f.press(t, rune1('r'))
		readUI(t, f.a.tapp, func() { testutil.Equal(t, f.a.mode, modeHeraInput) })
	})
	t.Run("worker row: delete asks to confirm", func(t *testing.T) {
		f := newResidentViewFixture(t)
		defer f.stop()
		f.selectWorker(t, 0)
		f.press(t, tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModCtrl))
		// The confirm (or merge-safety popup) may open a beat later (async preview).
		waitForCond(t, f.a.tapp, "delete prompt", func() bool { return f.a.mode != modeTaskList })
	})
	t.Run("worker row: role status steps", func(t *testing.T) {
		f := newResidentViewFixture(t)
		defer f.stop()
		f.selectWorker(t, 0)
		f.press(t, rune1('s'))
		st, err := f.d.HeraRoleStatusFor(f.roles[0])
		testutil.NoError(t, err)
		testutil.True(t, st != nil)
		f.press(t, rune1('S'))
		readUI(t, f.a.tapp, func() { testutil.Equal(t, f.a.mode, modeTaskList) })
	})
	t.Run("worker row: hide", func(t *testing.T) {
		f := newResidentViewFixture(t)
		defer f.stop()
		f.selectWorker(t, 0)
		f.press(t, rune1('a'))
		// A live worker asks for confirmation first; either way `a` acted on it.
		readUI(t, f.a.tapp, func() {
			r, err := f.d.HeraRole(f.roles[0])
			testutil.NoError(t, err)
			testutil.True(t, f.a.mode == modeHeraConfirm || r.ArchivedAt != nil)
		})
	})
}

func TestSmoke_ResidentView_InboxKeyOpensViewerFromAnyRow(t *testing.T) {
	for _, onWorker := range []bool{false, true} {
		f := newResidentViewFixture(t)
		if onWorker {
			f.selectWorker(t, 1)
		} else {
			f.selectResident(t)
		}
		f.press(t, rune1('i'))
		readUI(t, f.a.tapp, func() {
			testutil.Equal(t, f.a.mode, modeInbox)
			testutil.True(t, f.a.inboxModal != nil)
		})
		f.stop()
	}
}

// Clicking the non-interactive Details panel must not move focus off the page
// (tview's default Box mouse handling would, and the page would drop keys).
func TestSmoke_ResidentView_ClickKeepsFocus(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	f.selectResident(t)
	var x, y int
	readUI(t, f.a.tapp, func() {
		rx, ry, rw, rh := f.a.heraPage.GetRect()
		x, y = rx+rw-6, ry+rh/2 // inside the right-hand region
	})
	f.sim.InjectMouse(x, y, tcell.Button1, 0)
	syncUI(t, f.a.tapp)
	f.sim.InjectMouse(x, y, tcell.ButtonNone, 0)
	syncUI(t, f.a.tapp)
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
		testutil.Equal(t, f.a.tapp.GetFocus() == f.a.heraPage, true)
		testutil.Equal(t, f.a.mode, modeTaskList)
	})
	// Keys still reach the page afterwards.
	f.selectWorker(t, 0)
	f.press(t, rune1('r'))
	readUI(t, f.a.tapp, func() { testutil.Equal(t, f.a.mode, modeHeraInput) })
}

func TestSmoke_ResidentView_RailShowsNeedsInputAndUnread(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	icon, _ := widget.RoleStatusIcon(widget.RoleStatusInputs{NeedsInput: true, Live: true}, false, 0)
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.SetNeedsInput([]string{f.workers[1]})
		f.a.heraPage.Refresh()
		f.a.heraPage.SetUnread(map[string]int{f.workers[0]: 2})
	})
	redrawUI(t, f.a)
	readUI(t, f.a.tapp, func() {
		f.sim.Show()
		text := screenText(f.sim, 200, 40)
		var alpha, beta string
		for _, line := range strings.Split(text, "\n") {
			if r := []rune(line); len(r) > 34 {
				line = string(r[:34]) // the rail column only; the Details roster repeats names
			}
			switch {
			case strings.Contains(line, "alpha"):
				alpha = line
			case strings.Contains(line, "beta"):
				beta = line
			}
		}
		if alpha == "" || beta == "" {
			for i, line := range strings.Split(text, "\n") {
				t.Logf("%2d %s", i, line)
			}
		}
		testutil.True(t, alpha != "" && beta != "")
		testutil.Contains(t, alpha, "✉2")
		testutil.False(t, strings.Contains(beta, "✉"))
		testutil.Contains(t, beta, string(icon)) // the existing needs-input marker, per worker
		testutil.False(t, strings.Contains(alpha, string(icon)))
	})
}

// The resident pane keeps its own lifecycle: bound once with the first-attach
// resync, and never subject to the Hera size-drift kill+resume kick.
func TestSmoke_ResidentView_ResidentPaneNeverKicked(t *testing.T) {
	a, d := residentTestApp(t)
	sess := newRepaintSession()
	residentSession(t, a, d, sess)
	kicks := make(chan string, 8)
	a.heraPage.SetRerenderKicker(func(taskID string, _ uint16, _ func()) { kicks <- taskID })
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(200, 40)
	readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
	redrawUI(t, a)
	time.Sleep(hera.KickDebounce + 200*time.Millisecond)
	redrawUI(t, a)
	redrawUI(t, a)
	select {
	case id := <-kicks:
		t.Fatalf("resident pane was size-drift kicked for task %s", id)
	default:
	}
	readUI(t, a.tapp, func() {
		testutil.Equal(t, len(a.kickWait), 0)
		testutil.Equal(t, len(a.pendingRerenderRestart), 0)
	})
	readUI(t, a.tapp, func() { a.heraPage.SyncPanes() })
	testutil.True(t, sess.resizes.Load() >= 1) // first-attach resync still ran
}

// Hide keeps the worker on the resident rail (dimmed in place), and unhide
// restores it live — membership never rides on task_meta, which SetArchived wipes.
func TestSmoke_ResidentView_HideUnhideStaysOnRail(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	onRail := func() (found, archived bool) {
		readUI(t, f.a.tapp, func() {
			f.a.heraPage.Refresh()
			m := f.a.heraPage.Rail().Model()
			m.AnnotateRoles(func(rv *hera.RoleView) {
				if rv.TaskID == f.workers[0] {
					found, archived = true, rv.Archived
				}
			})
		})
		return
	}
	found, archived := onRail()
	testutil.True(t, found && !archived)
	testutil.NoError(t, f.d.ArchiveHeraRole(f.roles[0]))
	found, archived = onRail()
	testutil.True(t, found && archived)
	testutil.NoError(t, f.d.UnarchiveHeraRole(f.roles[0]))
	found, archived = onRail()
	testutil.True(t, found && !archived)
	yes, err := f.d.IsResidentWorker(f.workers[0])
	testutil.NoError(t, err)
	testutil.True(t, yes)
}
