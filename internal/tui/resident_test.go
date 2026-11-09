package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	dclient "github.com/argus-foundry/argus/internal/daemon/client"
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
	_, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	return New(d, agent.NewRunner(nil), false), d
}

type residentBlockingRunner struct {
	agent.SessionProvider
	entered chan struct{}
	release chan struct{}
	sess    agent.SessionHandle
}

func (r *residentBlockingRunner) Get(string) agent.SessionHandle {
	close(r.entered)
	<-r.release
	return r.sess
}

func TestSmoke_ResidentAttachDoesNotBlockUI(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			a, _ := residentTestApp(t)
			sess := newRepaintSession()
			r := &residentBlockingRunner{SessionProvider: a.runner, entered: make(chan struct{}), release: make(chan struct{}), sess: sess}
			a.runner = r
			a.daemonClient = &dclient.Client{} // unconnected; never touches a live daemon
			_, stop := wireApp(t, a)
			defer stop()
			readUI(t, a.tapp, func() { a.switchTab(widget.TabResident) })
			select {
			case <-r.entered:
			case <-time.After(uiTimeout):
				t.Fatal("attachment probe did not start")
			}
			// This UI round trip completes even with the daemon probe held.
			readUI(t, a.tapp, func() {
				testutil.True(t, a.residentAttachPending)
				a.refreshResidentTab() // in-flight probe suppresses duplicate work
				if stale {
					a.switchTab(widget.TabSettings)
				}
			})
			close(r.release)
			waitForCond(t, a.tapp, "resident attachment", func() bool { return !a.residentAttachPending })
			readUI(t, a.tapp, func() {
				if stale {
					testutil.Nil(t, a.residentPane.Session())
				} else {
					testutil.Equal(t, a.residentPane.Session() == sess, true)
				}
			})
		})
	}
}

func TestSmoke_ResidentPopupFocus(t *testing.T) {
	for _, popup := range []string{"palette", "switcher", "help", "error"} {
		t.Run(popup, func(t *testing.T) {
			a, _ := residentTestApp(t)
			a.toggleResident()
			sess := newRepaintSession()
			a.residentPane.SetSession(sess)
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
				testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.residentPane))
				a.residentPane.PasteHandler()("hello", func(tview.Primitive) {})
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
				testutil.Equal(t, a.tapp.GetFocus(), tview.Primitive(a.residentPane))
				_, _, width, _ := a.residentPane.GetRect()
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
	for _, mode := range []string{"modal", "filter", "settings edit"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := residentTestApp(t)
			switch mode {
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
			})
		})
	}
}

func TestSmoke_ResidentRuneBindingTypeableInTextInput(t *testing.T) {
	for _, mode := range []string{"filter", "settings edit", "modal"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := residentTestApp(t)
			km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "~"}})
			testutil.Equal(t, len(warnings), 0)
			a.keymap = km
			switch mode {
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
	// A rune binding is the only form keyenc can forward (it has no ctrl+\
	// encoding), and the pre-fix resolve swallowed it before the plugin saw it.
	km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "~"}})
	testutil.Equal(t, len(warnings), 0)
	readUI(t, a.tapp, func() { a.keymap = km })
	sim.InjectKey(tcell.KeyRune, '~', 0)
	syncUI(t, a.tapp)
	select {
	case b := <-plugIn:
		testutil.Equal(t, string(b), "~")
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
		a.residentPane.SetSession(sess)
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
	// Any representable override has the same reach as ctrl+backslash.
	km, warnings := keymap.Build(config.Keybindings{Global: map[string]string{"resident": "~"}})
	testutil.Equal(t, len(warnings), 0)
	a.keymap = km
	a.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '~', 0))
	testutil.Equal(t, a.header.ActiveTab(), widget.TabResident)
	a.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '~', 0))
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
