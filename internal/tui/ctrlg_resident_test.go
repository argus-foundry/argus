package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
)

// ctrl+g on the resident tab must not evaluate the team-only rail as if it
// were the Projects rail. A resident worker needing input is reached in place.
func TestSmoke_CtrlGResidentTabJumpsWithinTeam(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.SetNeedsInput([]string{f.workers[1]})
		f.a.heraPage.Refresh()
	})

	f.a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
	syncUI(t, f.a.tapp)

	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
		testutil.True(t, f.a.heraPage.ResidentMode())
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), f.workers[1])
		testutil.False(t, f.a.heraPage.HeldProjectsExcursion())
	})
}

// enterResidentFromProjects seeds two Projects orchestrators ("orch"/pw needs
// input later, "other"/ow), parks the Projects cursor on ow, optionally holds a
// Projects excursion, then opens the resident tab and selects a team worker.
func enterResidentFromProjects(t *testing.T, f *residentViewFixture, holdExcursion bool) {
	t.Helper()
	seedNeedsInputWorker(t, f.d, "orch", "pw")
	seedNeedsInputWorker(t, f.d, "other", "ow")
	readUI(t, f.a.tapp, func() {
		f.a.switchTab(widget.TabHera)
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
		testutil.True(t, f.a.heraPage.JumpToTask("ow"))
		if holdExcursion {
			f.a.heraPage.Rail().EnsureExcursionArmed()
		}
		f.a.switchTab(widget.TabResident)
	})
	f.selectWorker(t, 1)
}

// Nothing needs input anywhere: ctrl+g must not touch anything (tab, return
// target, focus, selected row, held Projects excursion).
func TestSmoke_CtrlGResidentTabNothingNeedsInputTouchesNothing(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	enterResidentFromProjects(t, f, true)
	ret := &residentReturn{tab: widget.TabTasks, taskID: "origin"}
	var focus hera.Focus
	readUI(t, f.a.tapp, func() {
		f.a.residentReturn = ret
		f.a.heraPage.Machine().ToRail()
		focus = f.a.heraPage.Machine().State()
		testutil.True(t, f.a.heraPage.HeldProjectsExcursion())
	})

	f.press(t, tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))

	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
		testutil.True(t, f.a.heraPage.ResidentMode())
		testutil.True(t, f.a.residentReturn == ret)
		testutil.Equal(t, f.a.heraPage.Machine().State(), focus)
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), f.workers[1])
		testutil.True(t, f.a.heraPage.HeldProjectsExcursion())
		testutil.Equal(t, f.a.header.Notice(), "No role needs input")
	})
}

// The fall-through jump arms the Projects excursion from the Projects rail's
// own pre-entry selection, so ctrl+b returns it there.
func TestSmoke_CtrlGResidentFallThroughRestoresProjectsSelection(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	enterResidentFromProjects(t, f, false)
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.SetNeedsInput([]string{"pw"})
		f.a.heraPage.Refresh()
	})

	f.press(t, tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabHera)
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), "pw")
		testutil.True(t, f.a.heraPage.Rail().HasExcursionSnapshot())
	})

	f.press(t, tcell.NewEventKey(tcell.KeyCtrlB, 0, 0))
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.Notice(), "Rail restored")
		// The rail's own selected row (the page's pane selection only
		// refreshes on the next rebuild, as for any ctrl+b).
		sel := f.a.heraPage.Rail().Selected()
		testutil.True(t, sel != nil)
		testutil.Equal(t, sel.TaskID, "ow")
	})
}

// With only a Projects role needing input, ctrl+g from the resident tab must
// land on Projects with that role selected, the excursion armed on the
// Projects rail, and ctrl+b must restore it.
func TestSmoke_CtrlGResidentTabFallsThroughToProjects(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	seedNeedsInputWorker(t, f.d, "orch", "pw")
	// Settle the Projects rail on a clean render so the 0->1 transition arms.
	readUI(t, f.a.tapp, func() {
		f.a.switchTab(widget.TabHera)
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
		f.a.switchTab(widget.TabResident)
		f.a.heraPage.SetNeedsInput([]string{"pw"})
		f.a.heraPage.Refresh()
		testutil.Equal(t, f.a.heraPage.Rail().NeedsInputCount(), 0) // team rail: pw is not on it
	})

	f.a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
	syncUI(t, f.a.tapp)

	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabHera)
		testutil.False(t, f.a.heraPage.ResidentMode())
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), "pw")
		testutil.True(t, f.a.heraPage.Rail().HasExcursionSnapshot())
	})

	f.a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlB, 0, 0))
	syncUI(t, f.a.tapp)
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.Notice(), "Rail restored")
		testutil.False(t, f.a.heraPage.Rail().HasExcursionSnapshot())
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabHera)
	})
}

// Nothing needs input anywhere: ctrl+g on the resident tab leaves the operator
// where they were.
func TestSmoke_CtrlGResidentTabNoopStaysPut(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	seedNeedsInputWorker(t, f.d, "orch", "pw")

	f.a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
	syncUI(t, f.a.tapp)

	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
		testutil.True(t, f.a.heraPage.ResidentMode())
		testutil.Equal(t, f.a.header.Notice(), "No role needs input")
	})
}

// A team worker interrupting while the resident tab shows must not leak into
// Projects as a fake excursion on an ordinary leave.
func TestSmoke_ResidentTeamInterruptionDoesNotLeakIntoProjects(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	seedNeedsInputWorker(t, f.d, "orch", "pw")
	readUI(t, f.a.tapp, func() {
		f.a.switchTab(widget.TabHera)
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
		f.a.switchTab(widget.TabResident)
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
		f.a.heraPage.SetNeedsInput([]string{f.workers[1]})
		f.a.heraPage.Refresh()
		f.a.switchTab(widget.TabHera)
		testutil.False(t, f.a.heraPage.ResidentMode())
		testutil.False(t, f.a.heraPage.HeldProjectsExcursion())
		testutil.False(t, f.a.heraPage.Rail().HasExcursionSnapshot())
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
	})
	var before string
	readUI(t, f.a.tapp, func() { before = f.a.heraPage.SelectionContext().TaskID() })
	f.press(t, tcell.NewEventKey(tcell.KeyCtrlG, 0, 0))
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.Notice(), "No role needs input")
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), before)
		testutil.False(t, f.a.heraPage.Rail().HasExcursionSnapshot())
	})
}

// A Projects excursion held across a resident round trip comes back.
func TestSmoke_ResidentRoundTripRestoresProjectsExcursion(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	enterResidentFromProjects(t, f, true)
	readUI(t, f.a.tapp, func() {
		f.a.switchTab(widget.TabHera)
		testutil.True(t, f.a.heraPage.JumpToTask("pw"))
		f.a.switchTab(widget.TabResident)
		f.a.switchTab(widget.TabHera)
		testutil.True(t, f.a.heraPage.Rail().HasExcursionSnapshot())
	})
	f.press(t, tcell.NewEventKey(tcell.KeyCtrlB, 0, 0))
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.header.Notice(), "Rail restored")
		sel := f.a.heraPage.Rail().Selected()
		testutil.True(t, sel != nil)
		testutil.Equal(t, sel.TaskID, "ow")
	})
}

// Entering the resident tab parks the Projects excursion off the shared rail:
// ctrl+b there must not restore it onto the team rail.
func TestSmoke_ResidentEntryParksProjectsExcursion(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	enterResidentFromProjects(t, f, true)
	readUI(t, f.a.tapp, func() {
		testutil.False(t, f.a.heraPage.Rail().HasExcursionSnapshot())
		testutil.True(t, f.a.heraPage.HeldProjectsExcursion())
	})
	f.press(t, tcell.NewEventKey(tcell.KeyCtrlB, 0, 0))
	readUI(t, f.a.tapp, func() {
		testutil.True(t, f.a.header.Notice() != "Rail restored")
		testutil.Equal(t, f.a.heraPage.SelectionContext().TaskID(), f.workers[1])
	})
}

// The needs-input baseline is restored on leaving resident mode so the team's
// empty set is not read as a fresh Projects interruption.
func TestSmoke_ResidentLeaveRestoresNeedsInputBaseline(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	seedNeedsInputWorker(t, f.d, "orch", "pw")
	readUI(t, f.a.tapp, func() {
		f.a.switchTab(widget.TabHera)
		f.a.heraPage.SetNeedsInput(nil)
		f.a.heraPage.Refresh()
		f.a.heraPage.SetNeedsInput([]string{"pw"})
		f.a.heraPage.Refresh()
		f.a.heraPage.Rail().RestoreExcursion()
		f.a.switchTab(widget.TabResident)
		f.a.switchTab(widget.TabHera)
		testutil.False(t, f.a.heraPage.Rail().HasExcursionSnapshot())
	})
}
