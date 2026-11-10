package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
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
