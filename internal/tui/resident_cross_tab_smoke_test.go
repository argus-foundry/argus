package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/store"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSmoke_ResidentCrossTabRoundTrip(t *testing.T) {
	for _, rail := range []bool{false, true} {
		name := "list"
		if rail {
			name = "rail"
		}
		t.Run(name, func(t *testing.T) {
			a, d := residentTestApp(t)
			sess := newRepaintSession()
			residentSession(t, a, d, sess)
			a.crossTabArrows = true
			sim, stop := wireApp(t, a)
			defer stop()
			sim.SetSize(200, 40)
			readUI(t, a.tapp, func() { a.setTasksRail(rail) })
			press := func(k tcell.Key) {
				sim.InjectKey(k, 0, tcell.ModAlt)
				syncUI(t, a.tapp)
			}
			press(tcell.KeyLeft)
			readUI(t, a.tapp, func() {
				testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
				testutil.True(t, a.heraPage.Machine().AtRightmost())
				testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusAgent)
				testutil.Equal(t, string(sess.RecentOutput()), "")
			})
			press(tcell.KeyLeft) // Worker/details pane -> resident's live PTY pane.
			readUI(t, a.tapp, func() {
				testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusCoord)
				testutil.Equal(t, a.heraPage.CoordPane().Session(), agentview.TerminalAdapter(sess))
			})
			press(tcell.KeyLeft) // Resident pane -> rail, never sent to the PTY.
			press(tcell.KeyRight)
			press(tcell.KeyRight)
			press(tcell.KeyRight)
			readUI(t, a.tapp, func() {
				testutil.Equal(t, string(sess.RecentOutput()), "")
				testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
				testutil.Equal(t, a.mode, modeTaskList)
				var want tview.Primitive = a.tasklist
				if rail {
					want = a.tasksRailPage
				}
				testutil.Equal(t, a.tapp.GetFocus(), want)
			})
		})
	}
}

func TestSmoke_ResidentCrossTabLadder(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	sess := newRepaintSession()
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.SetSessionResolver(func(string) agentview.TerminalAdapter { return sess })
	})
	readUI(t, f.a.tapp, func() {
		f.a.heraPage.OnReattach = nil // This test only exercises navigation.
	})
	f.selectWorker(t, 0)
	readUI(t, f.a.tapp, func() { f.a.crossTabArrows = true })
	press := func(k tcell.Key, want hera.Focus) {
		f.press(t, tcell.NewEventKey(k, 0, tcell.ModCtrl))
		readUI(t, f.a.tapp, func() {
			testutil.Equal(t, f.a.header.ActiveTab(), widget.TabResident)
			testutil.Equal(t, f.a.heraPage.Machine().State(), want)
		})
	}
	press(tcell.KeyLeft, hera.FocusRail) // left end consumes
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.heraPage.Rail().Selection().Role.RoleID, f.roles[0])
	})
	press(tcell.KeyRight, hera.FocusCoord)
	press(tcell.KeyRight, hera.FocusAgent)
	press(tcell.KeyLeft, hera.FocusCoord)
	press(tcell.KeyLeft, hera.FocusRail)
	testutil.Equal(t, string(sess.RecentOutput()), "")
}

func TestSmoke_ResidentCrossTabUnavailable(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "disabled"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			d := testDB(t)
			if remote {
				testutil.NoError(t, d.SetConfigValue("resident.enabled", "true"))
				testutil.NoError(t, d.SetConfigValue("resident.name", "Solo"))
			}
			var source store.Store = d
			if remote {
				source = nonRemoteStore{d}
			}
			a := New(source, agent.NewRunner(nil), false)
			a.crossTabArrows = true
			sim, stop := wireApp(t, a)
			defer stop()
			sim.InjectKey(tcell.KeyLeft, 0, tcell.ModAlt)
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
				testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.tasklist))
			})
		})
	}
}

func TestSmoke_ResidentCrossTabGuards(t *testing.T) {
	for _, guard := range []string{"disabled", "tasks filter", "resident filter", "fullscreen", "modal"} {
		t.Run(guard, func(t *testing.T) {
			a, _ := residentTestApp(t)
			a.crossTabArrows = guard != "disabled"
			sim, stop := wireApp(t, a)
			defer stop()
			sim.SetSize(200, 40)
			if guard != "tasks filter" {
				readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
			}
			switch guard {
			case "tasks filter", "resident filter":
				if guard == "resident filter" {
					readUI(t, a.tapp, func() { a.heraPage.Machine().ToRail() })
				}
				sim.InjectKey(tcell.KeyRune, '/', 0)
				syncUI(t, a.tapp)
			case "disabled":
				readUI(t, a.tapp, func() { a.heraPage.Machine().ToRightmost() })
			case "fullscreen":
				readUI(t, a.tapp, func() {
					a.heraPage.Machine().ToRightmost()
					a.heraPage.Machine().ToggleFullscreen()
				})
			case "modal":
				readUI(t, a.tapp, func() { a.openHelp() })
			}
			key := tcell.KeyRight
			if guard == "tasks filter" {
				key = tcell.KeyLeft
			}
			sim.InjectKey(key, 0, tcell.ModAlt)
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				want := widget.TabResident
				if guard == "tasks filter" {
					want = widget.TabTasks
				}
				testutil.Equal(t, a.header.ActiveTab(), want)
				switch guard {
				case "tasks filter":
					testutil.True(t, a.tasklist.Filtering())
				case "resident filter":
					testutil.True(t, a.heraPage.RailFiltering())
					testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusRail)
				case "fullscreen":
					testutil.True(t, a.heraPage.Machine().Fullscreen())
				case "modal":
					testutil.Equal(t, a.mode, modeHelp)
				}
			})
		})
	}
}

func TestSmoke_ResidentCrossTabHiddenPanes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		want  hera.Focus
	}{
		{"rail only", widget.RailWidth, hera.FocusRail},
		{"resident hidden", widget.RailWidth + 3, hera.FocusAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := residentTestApp(t)
			a.crossTabArrows = true
			sim, stop := wireApp(t, a)
			defer stop()
			sim.SetSize(tc.width, 24)
			press := func(k tcell.Key) {
				sim.InjectKey(k, 0, tcell.ModAlt)
				syncUI(t, a.tapp)
			}
			press(tcell.KeyLeft)
			redrawUI(t, a)
			readUI(t, a.tapp, func() {
				testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
				testutil.Equal(t, a.heraPage.Machine().State(), tc.want)
				testutil.True(t, a.heraPage.Machine().AtRightmost())
			})
			if tc.want == hera.FocusAgent {
				press(tcell.KeyLeft)
				readUI(t, a.tapp, func() { testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusRail) })
				press(tcell.KeyRight)
				readUI(t, a.tapp, func() { testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusAgent) })
			}
			press(tcell.KeyRight)
			readUI(t, a.tapp, func() { testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks) })
		})
	}
}
