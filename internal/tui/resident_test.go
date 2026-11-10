package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/argus-foundry/argus/internal/tui/views"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func residentTestApp(t *testing.T) (*App, *db.DB) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "true"))
	testutil.NoError(t, d.SetConfigValue("resident.name", "Solo"))
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	return New(d, agent.NewRunner(nil), false), d
}

// residentSession makes sess the live session the HeraPage resolver returns for
// the resident task (the resident pane is the page's coordinator pane in
// resident mode). When the page is already in resident mode it late-binds now.
func residentSession(t *testing.T, a *App, d *db.DB, sess agentview.TerminalAdapter) {
	t.Helper()
	row, err := d.ResidentTask()
	testutil.NoError(t, err)
	a.heraPage.SetSessionResolver(func(id string) agentview.TerminalAdapter {
		if id == row.ID && sess != nil {
			return sess
		}
		return nil
	})
	if a.heraPage.ResidentMode() {
		a.heraPage.Reconcile()
	}
}

func TestSmoke_ResidentPopupFocus(t *testing.T) {
	for _, popup := range []string{"palette", "switcher", "help", "error"} {
		t.Run(popup, func(t *testing.T) {
			a, d := residentTestApp(t)
			sess := newRepaintSession()
			residentSession(t, a, d, sess)
			a.toggleResident()
			_, stop := wireApp(t, a)
			defer stop()
			readUI(t, a.tapp, func() {
				switch popup {
				case "palette":
					a.openCommandPalette()
					a.closeCommandPaletteModal()
				case "switcher":
					a.openJumpSwitcher()
					a.closeJumpSwitcher()
				case "help":
					a.openHelp()
					a.closeHelp()
				case "error":
					a.showError("Oops", "test")
					a.closeErrorModal()
				}
				testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.heraPage))
				a.heraPage.CoordPane().PasteHandler()("hello", func(tview.Primitive) {})
				testutil.Equal(t, string(sess.ring.Tail(100)), "\x1b[200~hello\x1b[201~")
			})
		})
	}
}

func TestSmoke_ResidentRoundTrip(t *testing.T) {
	for _, from := range []string{"tasks", "agent", "files", "tasks rail", "hera", "hera coord", "settings"} {
		t.Run(from, func(t *testing.T) {
			a, d := residentTestApp(t)
			var orchID int64
			switch from {
			case "agent", "files", "tasks rail":
				task := &model.Task{ID: "origin", Name: "Origin", Status: model.StatusInProgress}
				testutil.NoError(t, d.Add(task))
				a.refreshTasks()
				a.onTaskSelect(task, false)
				a.setAgentZen()
				if from == "files" {
					a.clearAgentZen()
					a.agentFocus = focusFiles
					a.updateFocusIndicators()
				}
				if from == "tasks rail" {
					a.setTasksRail(true)
					a.focusTasksRail()
				}
			case "hera", "hera coord":
				orch, err := d.CreateHeraOrchestrator("origin", "")
				testutil.NoError(t, err)
				orchID = orch.ID
				if from == "hera coord" {
					testutil.NoError(t, d.Add(&model.Task{ID: "coord", Name: "Coord", Status: model.StatusInProgress}))
					role, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: orchID, Name: "coord", Kind: db.HeraKindCoordinator})
					testutil.NoError(t, err)
					_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: "coord", WorktreePath: t.TempDir()})
					testutil.NoError(t, err)
				}
				a.switchTab(widget.TabHera)
				testutil.True(t, a.heraPage.SelectOrch(orchID))
				a.heraPage.Machine().SetRegion(hera.FocusAgent)
				if from == "hera coord" {
					a.heraPage.Machine().SetRegion(hera.FocusCoord)
				}
				a.heraPage.Machine().ToggleFullscreen()
			case "settings":
				a.switchTab(widget.TabSettings)
				a.settings.setCategory(catAppearance)
				a.settings.FocusRightPane()
			}
			_, stop := wireApp(t, a)
			defer stop()
			a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, 0))
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
				testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.heraPage))
				_, _, width, _ := a.heraPage.GetRect()
				_, _, pageWidth, _ := a.pages.GetRect()
				testutil.Equal(t, width, pageWidth)
			})
			// Both exit keys must replay the same snapshot.
			key := tcell.KeyCtrlBackslash
			if from == "agent" || from == "settings" {
				key = tcell.KeyCtrlQ
			}
			a.tapp.QueueEvent(tcell.NewEventKey(key, 0, 0))
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				switch from {
				case "tasks":
					testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
					testutil.Equal(t, a.mode, modeTaskList)
				case "agent":
					testutil.Equal(t, a.mode, modeAgent)
					testutil.Equal(t, a.agentState.TaskID, "origin")
					testutil.True(t, a.agentZen)
				case "files":
					testutil.Equal(t, a.mode, modeAgent)
					testutil.False(t, a.agentZen)
					testutil.Equal(t, a.agentFocus, focusFiles)
				case "tasks rail":
					testutil.Equal(t, a.mode, modeTaskList)
					testutil.True(t, a.tasksRailViewing)
					testutil.Equal(t, a.agentState.TaskID, "origin")
					testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.tasksRailPage))
				case "hera", "hera coord":
					testutil.Equal(t, a.header.ActiveTab(), widget.TabHera)
					testutil.Equal(t, a.heraPage.Rail().SelectedOrch().ID, orchID)
					focus := hera.FocusAgent
					if from == "hera coord" {
						focus = hera.FocusCoord
					}
					testutil.Equal(t, a.heraPage.Machine().State(), focus)
					testutil.True(t, a.heraPage.Machine().Fullscreen())
				case "settings":
					testutil.Equal(t, a.header.ActiveTab(), widget.TabSettings)
					testutil.Equal(t, a.settings.category, catAppearance)
					testutil.True(t, a.settings.InRightPane())
				}
			})
		})
	}
}

func TestSmoke_ResidentIgnoredInModalAndFilter(t *testing.T) {
	for _, mode := range []string{"modal", "filter", "settings edit", "hera rail filter"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := residentTestApp(t)
			switch mode {
			case "hera rail filter":
				a.switchTab(widget.TabHera)
				a.heraPage.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '/', 0), func(tview.Primitive) {})
				testutil.True(t, a.heraPage.RailFiltering())
			case "modal":
				a.openHelp()
			case "filter":
				a.tasklist.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '/', 0), func(tview.Primitive) {})
			case "settings edit":
				a.switchTab(widget.TabSettings)
				a.settings.editingVault = "metis"
			}
			_, stop := wireApp(t, a)
			defer stop()
			a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, 0))
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				testutil.True(t, a.header.ActiveTab() != widget.TabResident)
				if mode == "modal" {
					testutil.Equal(t, a.mode, modeHelp)
				}
				if mode == "hera rail filter" {
					testutil.Equal(t, a.header.ActiveTab(), widget.TabHera)
					testutil.True(t, a.heraPage.RailFiltering())
				}
			})
		})
	}
}

func TestSmoke_ResidentRuneBindingTypeableInTextInput(t *testing.T) {
	for _, mode := range []string{"filter", "settings edit", "modal", "hera rail filter"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := residentTestApp(t)
			// A bare rune binding is rejected (Q10) so the default stays and '~'
			// is ordinary typing.
			km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "~"}})
			testutil.Equal(t, len(warnings), 1)
			a.keymap = km
			switch mode {
			case "hera rail filter":
				a.switchTab(widget.TabHera)
				a.heraPage.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '/', 0), func(tview.Primitive) {})
				testutil.True(t, a.heraPage.RailFiltering())
			case "modal":
				a.openHelp()
			case "filter":
				a.tasklist.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '/', 0), func(tview.Primitive) {})
			case "settings edit":
				a.switchTab(widget.TabSettings)
				selectRowInCategory(t, a.settings, catResident, srResident, resKeyName)
				testutil.True(t, a.settings.handleEnter())
			}
			_, stop := wireApp(t, a)
			defer stop()
			if mode == "settings edit" {
				readUI(t, a.tapp, func() { a.tapp.SetFocus(a.settingsPage) })
			}
			a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, '~', 0))
			syncUI(t, a.tapp)
			readUI(t, a.tapp, func() {
				testutil.True(t, a.header.ActiveTab() != widget.TabResident)
				switch mode {
				case "modal":
					testutil.Equal(t, a.mode, modeHelp) // swallowed, not dismissed
				case "filter":
					testutil.True(t, strings.HasSuffix(a.tasklist.Filter(), "~"))
				case "settings edit":
					testutil.True(t, strings.HasSuffix(a.settings.resEdit.buf, "~"))
				case "hera rail filter":
					testutil.Equal(t, a.header.ActiveTab(), widget.TabHera)
					testutil.True(t, a.heraPage.RailFiltering())
					testutil.True(t, strings.HasSuffix(a.heraPage.Rail().Filter(), "~"))
				}
			})
		})
	}
}

func TestSmoke_ResidentKeyForwardedInPluginView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := testDB(t)
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "true"))
	testutil.NoError(t, d.SetConfigValue("resident.name", "Solo"))
	_, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = views.New(d).Register("", "Ludwig", "ctrl+l", "ws://127.0.0.1:5111/ws")
	testutil.NoError(t, err)
	a := New(d, agent.NewRunner(nil), true)
	fake := &fakePluginConnector{}
	var plugIn <-chan []byte
	a.pluginConnFactory = func(url string, onBytes func([]byte), onControl func([]byte), in <-chan []byte) pluginConnector {
		fake.onBytes, fake.onControl, plugIn = onBytes, onControl, in
		return fake
	}
	a.loadPluginViews()
	sim, stop := wireApp(t, a)
	defer stop()
	sim.InjectKey(tcell.KeyCtrlL, 0, 0)
	syncUI(t, a.tapp)
	waitFor(t, time.Second, func() bool { return fake.dialed.Load() })
	// The default ctrl+\ has no keyenc encoding, so rebind to ctrl+x (bare runes
	// are rejected); the pre-fix resolve swallowed it before the plugin saw it.
	km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "ctrl+x"}})
	testutil.Equal(t, len(warnings), 0)
	readUI(t, a.tapp, func() { a.keymap = km })
	sim.InjectKey(tcell.KeyCtrlX, 0, 0)
	syncUI(t, a.tapp)
	select {
	case b := <-plugIn:
		testutil.Equal(t, string(b), "\x18")
	case <-time.After(time.Second):
		t.Fatal("the resident key was not forwarded to the plugin")
	}
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.mode, modePluginView)
		testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	})
}

func TestResidentTabIgnoresInvalidName(t *testing.T) {
	for name, bad := range map[string]string{"too long": strings.Repeat("x", config.ResidentNameMaxRunes+1), "control byte": "So\x1blo"} {
		t.Run(name, func(t *testing.T) {
			a, d := residentTestApp(t)
			testutil.NoError(t, d.SetConfigValue("resident.name", bad))
			a.refreshResidentTab()
			testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
			// A tab switch repaints the label without the tick; it must apply
			// the same validation.
			a.switchTab(widget.TabSettings)
			testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
			a.toggleResident()
			testutil.True(t, a.header.ActiveTab() != widget.TabResident)
			a.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '0', 0))
			testutil.True(t, a.header.ActiveTab() != widget.TabResident)
		})
	}
}

func TestResidentSettingsChangeAppliesBeforeTick(t *testing.T) {
	a, _ := residentTestApp(t)
	a.refreshResidentTab()
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
	residentEditField(t, a.settings, resKeyName, "Renamed")
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Renamed")
	selectRowInCategory(t, a.settings, catResident, srResident, resKeyEnabled)
	testutil.True(t, a.settings.handleEnter())
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
}

func TestResidentMarkViewedSeam(t *testing.T) {
	a, _ := residentTestApp(t)
	calls := make(chan struct{}, 4)
	a.residentMarkViewedFn = func() error { calls <- struct{}{}; return nil }
	_, stop := wireApp(t, a)
	defer stop()
	expect := func(want bool, what string) {
		t.Helper()
		select {
		case <-calls:
			if !want {
				t.Fatalf("%s: unexpected mark viewed", what)
			}
		case <-time.After(300 * time.Millisecond):
			if want {
				t.Fatalf("%s: mark viewed not called", what)
			}
		}
	}
	readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
	expect(true, "switchTab(resident)")
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{Unread: true}) })
	expect(true, "unread while viewing")
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabTasks)
		a.applyResidentState(daemon.ResidentStateResp{Unread: true})
	})
	expect(false, "unread while not viewing")
}

func TestSmoke_ResidentRoundTripKeepsCursors(t *testing.T) {
	t.Run("tasks list", func(t *testing.T) {
		a, d := residentTestApp(t)
		for _, n := range []string{"one", "two", "three"} {
			testutil.NoError(t, d.Add(&model.Task{ID: n, Name: n, Status: model.StatusInProgress}))
		}
		a.refreshTasks()
		a.tasklist.SelectByID("two")
		want := a.tasklist.SelectedTask().ID
		a.toggleResident()
		testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
		a.tasklist.SelectByID("one") // move the cursor while away
		testutil.Equal(t, a.tasklist.SelectedTask().ID, "one")
		a.toggleResident()
		testutil.Equal(t, a.tasklist.SelectedTask().ID, want)
	})
	t.Run("settings cursor", func(t *testing.T) {
		a, _ := residentTestApp(t)
		a.switchTab(widget.TabSettings)
		a.settings.setCategory(catAppearance)
		a.settings.FocusRightPane()
		testutil.True(t, len(a.settings.rows) > 2)
		a.settings.cursor = 2
		a.toggleResident()
		a.settings.cursor = 0 // anything the resident tab did in between
		a.toggleResident()
		testutil.Equal(t, a.header.ActiveTab(), widget.TabSettings)
		testutil.Equal(t, a.settings.cursor, 2)
	})
}

func TestSmoke_ResidentBoxDoesNotResizeHeraPanes(t *testing.T) {
	a, d := residentTestApp(t)
	orch, err := d.CreateHeraOrchestrator("origin", "")
	testutil.NoError(t, err)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(160, 40)
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabHera)
		testutil.True(t, a.heraPage.SelectOrch(orch.ID))
		a.heraPage.CoordPane().SetSession(newRepaintSession())
		a.heraPage.AgentPane().SetSession(newRepaintSession())
	})
	redrawUI(t, a)
	var coord, agentR, coord2, agent2 image
	readUI(t, a.tapp, func() {
		coord.x, coord.y, coord.w, coord.h = a.heraPage.CoordPane().GetRect()
		agentR.x, agentR.y, agentR.w, agentR.h = a.heraPage.AgentPane().GetRect()
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "hello"})
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Contains(t, screenText(sim, 160, 40), "Solo: hello")
		coord2.x, coord2.y, coord2.w, coord2.h = a.heraPage.CoordPane().GetRect()
		agent2.x, agent2.y, agent2.w, agent2.h = a.heraPage.AgentPane().GetRect()
		testutil.Equal(t, coord2, coord)
		testutil.Equal(t, agent2, agentR)
		testutil.Equal(t, len(a.kickWait), 0)
		testutil.Equal(t, len(a.pendingRerenderRestart), 0)
	})
}

func TestSmoke_ResidentDigitsRenameAndInput(t *testing.T) {
	a, d := residentTestApp(t)
	_, stop := wireApp(t, a)
	defer stop()
	a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, '0', 0))
	syncUI(t, a.tapp)
	sess := newRepaintSession()
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
		residentSession(t, a, d, sess)
	})
	for _, ev := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'q', 0),
		tcell.NewEventKey(tcell.KeyRune, '1', 0),
		tcell.NewEventKey(tcell.KeyCtrlC, 0, 0),
		tcell.NewEventKey(tcell.KeyCtrlL, 0, 0),
		tcell.NewEventKey(tcell.KeyCtrlZ, 0, 0),
		tcell.NewEventKey(tcell.KeyEnter, 0, 0),
	} {
		a.tapp.QueueEvent(ev)
	}
	syncUI(t, a.tapp)
	// Keys reach the session's ring asynchronously; under full-suite load the
	// control bytes can land after the UI queue drains, so wait for them.
	waitFor(t, 5*time.Second, func() bool {
		var got string
		readUI(t, a.tapp, func() { got = string(sess.ring.Tail(100)) })
		return got == "q1\x03\x0c\r"
	})
	readUI(t, a.tapp, func() {
		testutil.Equal(t, string(sess.ring.Tail(100)), "q1\x03\x0c\r")
		testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
		testutil.NoError(t, d.SetConfigValue("resident.name", "Renamed"))
		a.refreshResidentTab()
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Renamed")
		testutil.NoError(t, d.SetConfigValue("resident.enabled", "false"))
		a.refreshResidentTab()
		testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
	})
	// Disabled digit selection is a no-op and creates no task.
	a.tapp.QueueEvent(tcell.NewEventKey(tcell.KeyRune, '0', 0))
	syncUI(t, a.tapp)
	readUI(t, a.tapp, func() { testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks) })
}

func TestResidentReboundToggleAndMissingHera(t *testing.T) {
	a, d := residentTestApp(t)
	testutil.NoError(t, d.SetConfigValue("ui.last_tab", "resident"))
	a.restoreLastTab()
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	orch, err := d.CreateHeraOrchestrator("vanishing", "")
	testutil.NoError(t, err)
	a.switchTab(widget.TabHera)
	testutil.True(t, a.heraPage.SelectOrch(orch.ID))
	a.toggleResident()
	testutil.NoError(t, d.DeleteHeraOrchestrator(orch.ID))
	a.toggleResident()
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	// Any accepted override has the same reach as ctrl+backslash.
	km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "ctrl+x"}})
	testutil.Equal(t, len(warnings), 0)
	a.keymap = km
	a.handleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlX, 0, 0))
	testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
	a.handleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlX, 0, 0))
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	testutil.False(t, strings.Contains(a.header.TabLabel(widget.TabTasks), "resident"))
}

func TestResidentFallbackAndPersistence(t *testing.T) {
	a, d := residentTestApp(t)
	testutil.NoError(t, d.SaveLastTab("settings"))
	a.switchTab(widget.TabResident)
	last, err := d.LoadLastTab()
	testutil.NoError(t, err)
	testutil.Equal(t, last, "tasks")
	a.toggleResident()
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	task := &model.Task{ID: "gone", Name: "Gone", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	a.onTaskSelect(task, false)
	a.toggleResident()
	testutil.NoError(t, d.Delete(task.ID))
	a.toggleResident()
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
	testutil.Equal(t, a.mode, modeTaskList)
	// An ordinary tab selection forgets the hop.
	a.switchTab(widget.TabSettings)
	a.toggleResident()
	a.switchTab(widget.TabTasks)
	a.switchTab(widget.TabResident)
	a.toggleResident()
	testutil.Equal(t, a.header.ActiveTab(), widget.TabTasks)
}

func TestResidentLabelMarkerSurvivesTick(t *testing.T) {
	a, _ := residentTestApp(t)
	a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "hi"})
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ●")
	a.refreshResidentTab() // the tick's writer must not clobber the marker
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ●")
	a.applyResidentState(daemon.ResidentStateResp{Unread: true, Waiting: true})
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ?")
	a.applyResidentState(daemon.ResidentStateResp{})
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
	testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
}

func TestResidentDisabledShowsNothing(t *testing.T) {
	a, d := residentTestApp(t)
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "false"))
	a.applyResidentState(daemon.ResidentStateResp{Unread: true, Waiting: true})
	testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
	testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
}

func TestSmoke_ResidentBoxStacksWithAttentionBar(t *testing.T) {
	a, d := residentTestApp(t)
	a.header.SetTab(widget.TabTasks)
	for _, n := range []string{"alpha", "beta", "cur"} {
		testutil.NoError(t, d.Add(&model.Task{ID: n, Name: n, Status: model.StatusInProgress}))
	}
	a.refreshTasks()
	a.needsInputIDs = []string{"alpha", "beta"}
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(160, 40)
	readUI(t, a.tapp, func() {
		enterAgentViewFor(a, &model.Task{ID: "cur", Name: "cur"}, nil)
		a.agentPane.SetSession(newRepaintSession()) // live session: a resize could arm a kick
		a.pages.SwitchToPage("agent")
		a.clearAgentZen() // zen hides the left column
		a.updateAttentionBar()
	})
	redrawUI(t, a)
	var before image
	readUI(t, a.tapp, func() { before.x, before.y, before.w, before.h = a.agentPane.GetRect() })
	// Unread reply: both boxes visible together.
	readUI(t, a.tapp, func() {
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "all done"})
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		text := screenText(sim, 160, 40)
		testutil.Contains(t, text, "Needs input")
		testutil.Contains(t, text, "alpha")
		testutil.Contains(t, text, "● Solo: all done")
		testutil.Equal(t, a.residentBox.DesiredHeight(), 1)
		var after image
		after.x, after.y, after.w, after.h = a.agentPane.GetRect()
		testutil.Equal(t, after, before) // no PTY pane resize
		testutil.Equal(t, len(a.kickWait), 0)
		testutil.Equal(t, len(a.pendingRerenderRestart), 0)
	})
	// Waiting outranks unread.
	readUI(t, a.tapp, func() {
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, Waiting: true, LastReply: "ok?"})
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Contains(t, screenText(sim, 160, 40), "? Solo: ok?")
	})
	// Quiet: zero height, nothing drawn.
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{}) })
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
		testutil.True(t, !strings.Contains(screenText(sim, 160, 40), "Solo:"))
		var after image
		after.x, after.y, after.w, after.h = a.agentPane.GetRect()
		testutil.Equal(t, after, before)
	})
}

func redrawUI(t *testing.T, a *App) {
	t.Helper()
	a.tapp.QueueUpdateDraw(func() {})
	syncUI(t, a.tapp)
}

type image struct{ x, y, w, h int }

func TestSmoke_ResidentBoxHiddenOnResidentTabAndAckedWhileViewing(t *testing.T) {
	a, _ := residentTestApp(t)
	_, stop := wireApp(t, a)
	defer stop()
	readUI(t, a.tapp, func() {
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "x"})
		testutil.Equal(t, a.residentBox.DesiredHeight(), 1)
		a.switchTab(widget.TabResident)
		testutil.Equal(t, a.residentBox.DesiredHeight(), 0) // hidden on the resident tab
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
		// A reply arriving while the tab is focused is acked, not left as ●.
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "y"})
		testutil.Equal(t, a.residentSt.unread, false)
		testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
		a.switchTab(widget.TabSettings)
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
	})
}

func TestSmoke_ResidentPollOffUIThread(t *testing.T) {
	a, _ := residentTestApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	a.residentStateFn = func() (daemon.ResidentStateResp, error) {
		close(entered)
		<-release
		return daemon.ResidentStateResp{Waiting: true}, nil
	}
	_, stop := wireApp(t, a)
	defer stop()
	readUI(t, a.tapp, func() { a.pollResidentState() })
	select {
	case <-entered:
	case <-time.After(uiTimeout):
		t.Fatal("poll did not start")
	}
	readUI(t, a.tapp, func() { // UI stays responsive; single flight
		testutil.True(t, a.residentPollPending)
		a.residentPolledAt = time.Time{}
		a.pollResidentState()
	})
	close(release)
	waitForCond(t, a.tapp, "poll applied", func() bool { return a.residentSt.waiting })
	readUI(t, a.tapp, func() { testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ?") })
}

func TestSmoke_ResidentDisabledStillPollsWorkerCount(t *testing.T) {
	a, d := residentTestApp(t)
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "false"))
	a.residentStateFn = func() (daemon.ResidentStateResp, error) {
		return daemon.ResidentStateResp{RunningWorkers: 2}, nil
	}
	_, stop := wireApp(t, a)
	defer stop()
	readUI(t, a.tapp, func() {
		a.refreshResidentTab()
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "")
		a.pollResidentState()
	})
	waitForCond(t, a.tapp, "disabled worker count", func() bool { return a.settings.residentRunningWorkers == 2 })
	readUI(t, a.tapp, func() {
		a.settings.setCategory(catResident)
		selectRowInCategory(t, a.settings, catResident, srResident, resKeyEnabled)
		testutil.Contains(t, a.settings.SelectedRow().label, "2 workers still running")
	})
}

func TestSmoke_ResidentHeraAndTasksRailBox(t *testing.T) {
	a, _ := residentTestApp(t)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(160, 40)
	readUI(t, a.tapp, func() {
		a.applyResidentState(daemon.ResidentStateResp{Waiting: true, LastReply: "confirm?"})
		a.switchTab(widget.TabHera)
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Contains(t, screenText(sim, 160, 40), "? Solo: confirm?")
		a.switchTab(widget.TabTasks)
		a.setTasksRail(true)
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Contains(t, screenText(sim, 160, 40), "? Solo: confirm?")
	})
}

// The Tasks-rail ResidentBox is drawn only when no task is bound (the task list
// is showing); with a bound task the agent view's own stacking covers it.
func TestSmoke_ResidentBoxOnTasksRailWhenUnbound(t *testing.T) {
	a, _ := residentTestApp(t)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(160, 40)
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabTasks)
		a.setTasksRail(true)
		a.applyResidentState(daemon.ResidentStateResp{Unread: true, LastReply: "rail reply"})
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Contains(t, screenText(sim, 160, 40), "● Solo: rail reply")
		a.tasksRailPage.Bound = true // a bound task: agent view stacking owns the box
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.True(t, !strings.Contains(screenText(sim, 160, 40), "rail reply"))
		a.tasksRailPage.Bound = false
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{}) })
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.True(t, !strings.Contains(screenText(sim, 160, 40), "Solo:"))
	})
}

// TestSmoke_ResidentReturnIsDirect: toggling back lands on the origin with ONE
// switchTab (no intermediate Tasks hop, so no double persist/box sync).
func TestSmoke_ResidentReturnIsDirect(t *testing.T) {
	for _, from := range []string{"tasks", "agent", "hera", "hera focus mismatch", "settings"} {
		t.Run(from, func(t *testing.T) {
			a, d := residentTestApp(t)
			var coordTask string
			want := widget.TabTasks
			switch from {
			case "agent":
				task := &model.Task{ID: "origin", Name: "Origin", Status: model.StatusInProgress}
				testutil.NoError(t, d.Add(task))
				a.refreshTasks()
				a.onTaskSelect(task, false)
			case "hera", "hera focus mismatch":
				want = widget.TabHera
				orch, err := d.CreateHeraOrchestrator("origin", "")
				testutil.NoError(t, err)
				if from == "hera focus mismatch" {
					coordTask = "coord"
					testutil.NoError(t, d.Add(&model.Task{ID: "coord", Name: "Coord", Status: model.StatusInProgress}))
					role, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator})
					testutil.NoError(t, err)
					_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: "coord", WorktreePath: t.TempDir()})
					testutil.NoError(t, err)
				}
				a.switchTab(widget.TabHera)
				testutil.True(t, a.heraPage.SelectOrch(orch.ID))
				if coordTask != "" {
					a.heraPage.Machine().SetRegion(hera.FocusCoord)
					testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusCoord)
				}
			case "settings":
				want = widget.TabSettings
				a.switchTab(widget.TabSettings)
				a.settings.setCategory(catAppearance)
			}
			a.toggleResident()
			testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
			if coordTask != "" {
				// The coordinator pane leaves the layout while away (narrow
				// terminal); the selected orchestrator stays.
				a.heraPage.Machine().SetCoordPresent(false)
			}
			var hops, persists, headerSets []widget.Tab
			a.onSwitchTab = func(tab widget.Tab) { hops = append(hops, tab) }
			a.onPersistLastTab = func(tab widget.Tab) { persists = append(persists, tab) }
			a.header.SetTabObserver(func(tab widget.Tab) { headerSets = append(headerSets, tab) })
			a.toggleResident()
			testutil.DeepEqual(t, hops, []widget.Tab{want})
			// A detour that bypasses the onSwitchTab seam (header.SetTab +
			// persistLastTab straight to Tasks) would show up here.
			testutil.DeepEqual(t, persists, []widget.Tab{want})
			testutil.DeepEqual(t, headerSets, []widget.Tab{want})
			testutil.Equal(t, a.header.ActiveTab(), want)
			last, err := d.LoadLastTab()
			testutil.NoError(t, err)
			testutil.Equal(t, last, lastTabString(want))
			if from == "agent" {
				testutil.Equal(t, a.mode, modeAgent)
			}
			if coordTask != "" {
				testutil.NotEqual(t, a.heraPage.Machine().State(), hera.FocusCoord)
			}
		})
	}
}

// TestSmoke_ResidentFirstAttachResizesPTY: a freshly bound live session is
// resized to the pane even though the pane's seeded size already matches (no
// width delta), via the one-shot forced resync (Q7) — now performed by the
// HeraPage's bindPane when the resident tab enters resident mode. Re-ticking
// the SAME session must not resize again, and no size-drift kick is armed for
// the resident pane (the daemon owns its lifecycle).
func TestSmoke_ResidentFirstAttachResizesPTY(t *testing.T) {
	a, d := residentTestApp(t)
	first := newRepaintSession()
	residentSession(t, a, d, first)
	_, stop := wireApp(t, a)
	defer stop()
	readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
	waitForCond(t, a.tapp, "first bound", func() bool { return a.heraPage.CoordPane().Session() != nil })
	redrawUI(t, a)
	readUI(t, a.tapp, func() { a.heraPage.SyncPanes() })
	testutil.True(t, first.resizes.Load() >= 1)
	// Re-ticking the SAME session must not resize again.
	before := first.resizes.Load()
	for i := 0; i < 3; i++ {
		readUI(t, a.tapp, func() { a.refreshResidentTab(); a.heraPage.Reconcile() })
		redrawUI(t, a)
		readUI(t, a.tapp, func() { a.heraPage.SyncPanes() })
	}
	testutil.Equal(t, first.resizes.Load(), before)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, len(a.kickWait), 0)
		testutil.Equal(t, len(a.pendingRerenderRestart), 0)
	})
}

// TestSmoke_ResidentReasonShownInPlaceholderAndSettings: the daemon's reason
// reaches the resident tab's no-session placeholder and Settings -> Resident,
// control bytes stripped, and clears when the daemon reports none (Q11).
func TestSmoke_ResidentReasonShownInPlaceholderAndSettings(t *testing.T) {
	a, _ := residentTestApp(t)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(240, 40) // the resident pane is the Hera coordinator pane: give the banner room
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.applyResidentState(daemon.ResidentStateResp{Reason: "waiting for the Argus MCP server\x1b[31m"})
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.residentSt.reason, "waiting for the Argus MCP server [31m")
		sim.Show()
		testutil.Contains(t, screenText(sim, 240, 40), "waiting for the Argus MCP server")
	})
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabSettings)
		selectRowInCategory(t, a.settings, catResident, srResident, resKeyName)
		a.settings.SetRect(0, 0, 100, 30)
		s2 := drawSim(t)
		a.settings.Draw(s2)
		s2.Show()
		testutil.Contains(t, screenText(s2, 80, 24), "Not running: waiting for the Argus MCP server")
		a.applyResidentState(daemon.ResidentStateResp{})
		testutil.Equal(t, a.settings.residentReason, "")
		testutil.Equal(t, a.residentSt.reason, "")
		s3 := drawSim(t)
		a.settings.Draw(s3)
		s3.Show()
		testutil.False(t, strings.Contains(screenText(s3, 80, 24), "waiting for the Argus MCP server"))
		a.switchTab(widget.TabResident)
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		sim.Show()
		testutil.False(t, strings.Contains(screenText(sim, 240, 40), "waiting for the Argus MCP server"))
	})
}

// TestSmoke_ResidentRunningDropsReason: the daemon reports prerequisite reasons
// even while the session runs; a running resident shows no "Not running:" line
// in Settings and no placeholder note.
func TestSmoke_ResidentRunningDropsReason(t *testing.T) {
	a, _ := residentTestApp(t)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(240, 40)
	const reason = "prereq-reason-xyz"
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.applyResidentState(daemon.ResidentStateResp{Running: true, Reason: reason})
		testutil.Equal(t, a.residentSt.reason, "")
		testutil.Equal(t, a.settings.residentReason, "")
	})
	redrawUI(t, a)
	readUI(t, a.tapp, func() {
		sim.Show()
		testutil.False(t, strings.Contains(screenText(sim, 240, 40), reason))
		a.switchTab(widget.TabSettings)
		selectRowInCategory(t, a.settings, catResident, srResident, resKeyName)
		a.settings.SetRect(0, 0, 100, 30)
		s2 := drawSim(t)
		a.settings.Draw(s2)
		s2.Show()
		text := screenText(s2, 80, 24)
		testutil.False(t, strings.Contains(text, "Not running:"))
		testutil.False(t, strings.Contains(text, reason))
		// Control: the same reason while NOT running is shown.
		a.applyResidentState(daemon.ResidentStateResp{Running: false, Reason: reason})
		s3 := drawSim(t)
		a.settings.Draw(s3)
		s3.Show()
		testutil.Contains(t, screenText(s3, 80, 24), "Not running: "+reason)
	})
}

// writeResidentLog seeds the dead resident's session log so its tab replays it
// (instead of drawing the "Session not running" placeholder).
func writeResidentLog(t *testing.T, d *db.DB) {
	t.Helper()
	row, err := d.ResidentTask()
	testutil.NoError(t, err)
	home := os.Getenv("HOME")
	testutil.NoError(t, os.MkdirAll(filepath.Join(home, ".argus", "sessions"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(home, ".argus", "sessions", row.ID+".log"),
		[]byte(strings.Repeat("replayed resident output\r\n", 30)), 0o644))
}

// TestSmoke_ResidentReasonBannerOverReplay: once the resident has run, its
// stopped tab replays the dead session's log, so the daemon's not-running
// reason shows as a one-line banner over the replay; it is absent when the
// resident runs or the reason clears.
func TestSmoke_ResidentReasonBannerOverReplay(t *testing.T) {
	a, d := residentTestApp(t)
	writeResidentLog(t, d)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(240, 40)
	const reason = "waiting for the Argus MCP server"
	show := func() string {
		redrawUI(t, a)
		var text string
		readUI(t, a.tapp, func() {
			sim.Show()
			text = screenText(sim, 240, 40)
		})
		return text
	}
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.applyResidentState(daemon.ResidentStateResp{Reason: reason})
	})
	waitForCond(t, a.tapp, "replay loaded", func() bool { return a.heraPage.CoordPane().HasContent() })
	text := show()
	testutil.Contains(t, text, reason)
	testutil.False(t, strings.Contains(text, "Session not running"))
	// Running: the reason is dropped, so the banner goes with it.
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{Running: true, Reason: reason}) })
	testutil.False(t, strings.Contains(show(), reason))
	// Not running again, then the reason clears.
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{Reason: reason}) })
	testutil.Contains(t, show(), reason)
	readUI(t, a.tapp, func() { a.applyResidentState(daemon.ResidentStateResp{}) })
	testutil.False(t, strings.Contains(show(), reason))
}

// TestInitialPTYLayout_ResidentMatchesPaneRect: the published resident size is
// the resident pane's real inner rect (full width minus border, between the
// header and status bar) and is distinct from Active on a narrower view.
func TestInitialPTYLayout_ResidentMatchesPaneRect(t *testing.T) {
	for _, size := range [][2]int{{239, 40}, {120, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a, d := residentTestApp(t)
			screen, stop := wireApp(t, a)
			defer stop()
			readUI(t, a.tapp, func() {
				screen.SetSize(size[0], size[1])
				a.switchTab(widget.TabHera) // Active = a narrower worker pane
			})
			a.tapp.QueueUpdateDraw(func() {})
			waitRerender(t, func() bool {
				s, err := d.InitialPTYSize(db.InitialPTYRoleResident, time.Now())
				return err == nil && s.Valid()
			})
			readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
			redrawUI(t, a)
			var want db.PTYSize
			readUI(t, a.tapp, func() {
				_, _, w, h := a.heraPage.CoordPane().GetRect()
				want = db.PTYSize{Rows: uint16(h - 2), Cols: uint16(w - 2)} //nolint:gosec // simulation-screen pane dims; pane draws its own 1-cell border
			})
			waitRerender(t, func() bool {
				s, err := d.InitialPTYSize(db.InitialPTYRoleResident, time.Now())
				return err == nil && s == want
			})
			active, err := d.InitialPTYSize("", time.Now())
			testutil.NoError(t, err)
			testutil.Equal(t, active, want) // on the resident tab Active is the same full-width pane
		})
	}
}

// The resident size is independent of the active view: while on the Hera tab
// Active is the (narrow) worker pane but Resident stays the full-width pane.
func TestInitialPTYLayout_ResidentIndependentOfActive(t *testing.T) {
	a, d := residentTestApp(t)
	screen := tcell.NewSimulationScreen("")
	testutil.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(239, 40)
	a.nowFn = time.Now
	a.header.SetTab(widget.TabHera)
	a.publishInitialPTYLayout(screen)
	waitPTYPublication(t, &a.initialPTYPublisher)
	res, err := d.InitialPTYSize(db.InitialPTYRoleResident, time.Now())
	testutil.NoError(t, err)
	active, err := d.InitialPTYSize("", time.Now())
	testutil.NoError(t, err)
	testutil.Equal(t, res, db.PTYSize{Rows: 36, Cols: 100}) // the resident pane is the always-split coordinator pane
	testutil.Equal(t, active, db.PTYSize{Rows: 36, Cols: 101})
}
