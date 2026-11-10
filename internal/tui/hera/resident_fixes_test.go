package hera

import (
	"testing"

	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/gdamore/tcell/v2"
)

// The resident's session belongs to the daemon's ensure loop: every forced
// re-render entry point (key, owed-notice click, palette) is inert for it, but
// worker panes keep ctrl+n.
func TestResidentMode_ForcedRerenderInertForResident(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	residentTask, err := f.d.ResidentTask()
	testutil.NoError(t, err)
	var got []string
	f.p.OnRerender = func(id string) { got = append(got, id) }
	h := f.p.InputHandler()
	ctrlN := tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModCtrl)

	f.p.Machine().SetRegion(FocusCoord)
	testutil.Equal(t, f.p.FocusedTerminalTaskID(), residentTask.ID)
	h(ctrlN, noFocus)
	f.p.SetOwedFunc(nil, nil)
	f.p.coordPane.OnOwedClick()
	f.p.FireRerender(residentTask.ID)
	testutil.Equal(t, len(got), 0)
	testutil.False(t, f.p.RerenderAllowed(residentTask.ID))

	f.selectTask(t, f.workers[0])
	f.p.Machine().SetRegion(FocusAgent)
	testutil.True(t, f.p.RerenderAllowed(f.workers[0]))
	h(ctrlN, noFocus)
	testutil.DeepEqual(t, got, []string{f.workers[0]})
	f.p.agentPane.OnOwedClick()
	testutil.Equal(t, len(got), 2)
}

// A just-graduated worker's ENDED role row ("graduation pending restart") has
// no live binding: its task now belongs to the graduate, so every management
// key and Enter is inert there while live worker rows still work.
func TestResidentMode_GraduationPendingRowInert(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	role := roleForTask(t, f.d, f.orchID, f.workers[0])
	binds, err := f.d.ListHeraBindingsByRole(role.ID)
	testutil.NoError(t, err)
	_, err = f.d.GraduateHeraWorker(binds[0].ID)
	testutil.NoError(t, err)
	f.p.Refresh()
	f.p.SetResidentMode(false)
	f.p.SetResidentMode(true)

	// Locate the pending row on the rail.
	var pending *heramodel.RoleView
	for _, o := range f.p.Rail().Model().Active {
		for i := range o.Roles {
			if o.Roles[i].GraduationPendingRestart {
				pending = &o.Roles[i]
			}
		}
	}
	testutil.True(t, pending != nil)
	calls := f.wire()
	f.p.Machine().ToRail()
	nav := f.p.InputHandler()
	for i := 0; i < 20; i++ {
		if r := f.p.Rail().Selection().Role; r != nil && r.GraduationPendingRestart {
			break
		}
		nav(key('j'), noFocus)
	}
	sel := f.p.Rail().Selection()
	testutil.True(t, sel.Role != nil && sel.Role.GraduationPendingRestart)
	testutil.True(t, ResidentRowInert(sel))
	for _, act := range []keymap.Action{keymap.ActHeraDelete, keymap.ActHeraRename, keymap.ActHeraArchive, keymap.ActHeraStatAdv, keymap.ActHeraStatRev} {
		testutil.False(t, ResidentRailAllows(act, sel))
	}
	h := f.p.InputHandler()
	clear(calls.n)
	for _, ev := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModCtrl), key('r'), key('a'), key('s'), key('S'),
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
	} {
		h(ev, noFocus)
	}
	testutil.Equal(t, len(calls.n), 0)

	// A live worker row is unaffected.
	f.selectTask(t, f.workers[1])
	h(key('r'), noFocus)
	testutil.Equal(t, calls.n["rename"], 1)
}

// selectPendingGraduate graduates the first worker and selects its ended
// "graduation pending restart" row, returning the graduate's task id.
func selectPendingGraduate(t *testing.T, f *residentFixture) string {
	t.Helper()
	role := roleForTask(t, f.d, f.orchID, f.workers[0])
	binds, err := f.d.ListHeraBindingsByRole(role.ID)
	testutil.NoError(t, err)
	_, err = f.d.GraduateHeraWorker(binds[0].ID)
	testutil.NoError(t, err)
	f.p.Refresh()
	f.p.SetResidentMode(false)
	f.p.SetResidentMode(true)
	f.p.Machine().ToRail()
	nav := f.p.InputHandler()
	for i := 0; i < 20; i++ {
		if r := f.p.Rail().Selection().Role; r != nil && r.GraduationPendingRestart {
			break
		}
		nav(key('j'), noFocus)
	}
	r := f.p.Rail().Selection().Role
	testutil.True(t, r != nil && r.GraduationPendingRestart)
	return f.workers[0]
}

// The graduation-pending row's task belongs to the daemon-owned graduation
// restart: ctrl+n, the owed-notice click, the palette row and the automatic
// size-drift kick must all refuse it (a double restart otherwise), while a live
// worker row keeps every one of them.
func TestResidentMode_PendingGraduateNoForcedRerenderOrKick(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	grad := selectPendingGraduate(t, f)
	var got []string
	f.p.OnRerender = func(id string) { got = append(got, id) }
	kicks, advance := newKickRecorder(f.p)
	f.p.agentBound = grad // the pane still binds the pending row's task

	f.p.Machine().SetRegion(FocusAgent)
	f.p.InputHandler()(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModCtrl), noFocus)
	f.p.SetOwedFunc(nil, nil)
	f.p.agentPane.OnOwedClick()
	f.p.FireRerender(grad)
	testutil.Equal(t, len(got), 0)
	testutil.False(t, f.p.RerenderAllowed(grad))

	advance(KickDebounce)
	f.p.maybeKickPaneRerender(f.p.agentBound, &f.p.agentKickedFor, 100, &f.p.agentKickPending, false)
	advance(KickDebounce * 2)
	f.p.maybeKickPaneRerender(f.p.agentBound, &f.p.agentKickedFor, 100, &f.p.agentKickPending, false)
	testutil.Equal(t, len(*kicks), 0)

	// Control: a live worker row still kicks and rerenders.
	f.selectTask(t, f.workers[1])
	f.p.agentBound = f.workers[1]
	testutil.True(t, f.p.RerenderAllowed(f.workers[1]))
	advance(KickDebounce)
	f.p.maybeKickPaneRerender(f.p.agentBound, &f.p.agentKickedFor, 100, &f.p.agentKickPending, false)
	advance(KickDebounce * 2)
	f.p.maybeKickPaneRerender(f.p.agentBound, &f.p.agentKickedFor, 100, &f.p.agentKickPending, false)
	testutil.Equal(t, len(kicksFor(*kicks, f.workers[1])), 1)
}

// The pane-level reattach (click / Enter in the agent pane) is skipped for the
// pending row; it still fires for a live worker.
func TestResidentMode_PendingGraduatePaneReattachSkipped(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	grad := selectPendingGraduate(t, f)
	calls := f.wire()
	f.p.agentBound = grad
	f.p.reattachPane(f.p.agentPane)
	testutil.Equal(t, calls.n["reattach"], 0)

	f.selectTask(t, f.workers[1])
	before := calls.n["reattach"]
	f.p.reattachPane(f.p.agentPane)
	testutil.Equal(t, calls.n["reattach"], before+1)
}
