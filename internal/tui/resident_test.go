package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/config"
	dclient "github.com/drn/argus/internal/daemon/client"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/tui/hera"
	"github.com/drn/argus/internal/tui/keymap"
	"github.com/drn/argus/internal/tui/widget"
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
