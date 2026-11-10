package tui

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func containsLabel(rows []paletteRow, label string) bool {
	for _, r := range rows {
		if r.Label == label {
			return true
		}
	}
	return false
}

// --- CommandPaletteModal (widget-level) ---

func TestCommandPaletteModal_FilterNarrowsPreservingOrder(t *testing.T) {
	rows := []paletteRow{
		{Label: "new task", Key: "n", invoke: func() {}},
		{Label: "switch to Projects tab", Key: "2", invoke: func() {}},
		{Label: "toggle pin", Key: "P", invoke: func() {}},
	}
	m := NewCommandPaletteModal(rows)
	h := m.InputHandler()
	for _, r := range "proj" {
		h(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(tview.Primitive) {})
	}
	testutil.Equal(t, len(m.filtered), 1)
	testutil.Equal(t, m.filtered[0].Label, "switch to Projects tab")
}

func TestCommandPaletteModal_EnterInvokesSelectedAndClosesViaApp(t *testing.T) {
	rows := []paletteRow{
		{Label: "a", Key: "a", invoke: func() {}},
	}
	m := NewCommandPaletteModal(rows)
	h := m.InputHandler()
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	testutil.Equal(t, m.Selected(), true)
	invoked := false
	m.filtered[0].invoke = func() { invoked = true }
	m.Invoke()
	testutil.Equal(t, invoked, true)
}

func TestCommandPaletteModal_EscCancels(t *testing.T) {
	m := NewCommandPaletteModal([]paletteRow{{Label: "a", Key: "a", invoke: func() {}}})
	h := m.InputHandler()
	h(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
	testutil.Equal(t, m.Canceled(), true)
	testutil.Equal(t, m.Selected(), false)
}

func TestCommandPaletteModal_EnterOnEmptyFilteredIsNoop(t *testing.T) {
	m := NewCommandPaletteModal([]paletteRow{{Label: "a", Key: "a", invoke: func() {}}})
	h := m.InputHandler()
	h(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone), func(tview.Primitive) {})
	testutil.Equal(t, len(m.filtered), 0)
	h(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	testutil.Equal(t, m.Selected(), false) // nothing to select
}

// --- paletteApplicableActions (App-level, no sim needed) ---

func TestPaletteApplicableActions_ClassicAgentViewIsCtxAgentOnly(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeAgent

	rows := app.paletteApplicableActions()
	testutil.Equal(t, containsLabel(rows, "toggle single-pane (zoom)"), true)
	// resize-without-restart: the on-demand re-render is a CtxAgent action.
	testutil.Equal(t, containsLabel(rows, "re-render at this width"), true)
	// The jump switcher moved to CtxGlobal (add-two-column-switcher) but still
	// fires in the agent view, so it stays offered here — the one global row.
	testutil.Equal(t, containsLabel(rows, "jump switcher (tasks + projects)"), true)
	// The pre-existing modeAgent CtxGlobal boundary is untouched: no global
	// actions (quit/tab-switch/help) leak into the agent-view palette.
	testutil.Equal(t, containsLabel(rows, "quit"), false)
	testutil.Equal(t, containsLabel(rows, "switch to Tasks tab"), false)
	testutil.Equal(t, containsLabel(rows, "next needs-input (?) / unread coordinator board"), false)

	// Exactly one global-context row: the switcher. Labels shared with an
	// agent-context action (e.g. a same-named agent action) are not global rows.
	km := app.activeKeymap()
	agentLabels := map[string]bool{}
	for _, act := range keymap.ContextOrder(keymap.CtxAgent) {
		agentLabels[km.ActionLabel(act)] = true
	}
	globalLabels := map[string]bool{}
	for _, act := range keymap.ContextOrder(keymap.CtxGlobal) {
		if l := km.ActionLabel(act); !agentLabels[l] {
			globalLabels[l] = true
		}
	}
	var globalRows []string
	for _, r := range rows {
		if globalLabels[r.Label] {
			globalRows = append(globalRows, r.Label)
		}
	}
	testutil.DeepEqual(t, globalRows, []string{"jump switcher (tasks + projects)"})
}

func TestPaletteApplicableActions_TaskListIsGlobalPlusTaskList(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeTaskList
	app.header.SetTab(widget.TabTasks)

	rows := app.paletteApplicableActions()
	testutil.Equal(t, containsLabel(rows, "quit"), true)
	testutil.Equal(t, containsLabel(rows, "new task"), true)
	// A new CtxGlobal action (add-hera-jump-question) is reachable here too,
	// like every other global action.
	testutil.Equal(t, containsLabel(rows, "next needs-input (?) / unread coordinator board"), true)
	// No cross-tab bleed: Hera rail actions absent from the Tasks-tab palette.
	testutil.Equal(t, containsLabel(rows, "spawn worker under coordinator (new-task modal)"), false)
}

func TestPaletteApplicableActions_HeraRailIsGlobalPlusHeraRail(t *testing.T) {
	d := testDB(t)
	orch := seedHeraOrch(t, d, "orch")
	seedHeraBoundRole(t, d, orch, "coord", db.HeraKindCoordinator, "tc")
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeTaskList
	app.header.SetTab(widget.TabHera)
	app.heraPage.Refresh()

	rows := app.paletteApplicableActions()
	testutil.Equal(t, containsLabel(rows, "quit"), true)
	testutil.Equal(t, containsLabel(rows, "spawn worker under coordinator (new-task modal)"), true)
	// No cross-tab bleed: Tasks-tab-only actions absent.
	testutil.Equal(t, containsLabel(rows, "new task"), false)
	// Rail focus (not a pane) — no Hera literal actions (fullscreen/copy).
	testutil.Equal(t, containsLabel(rows, "toggle fullscreen"), false)
	testutil.Equal(t, containsLabel(rows, "re-render at this width"), false)
}

func TestPaletteApplicableActions_HeraPaneIncludesLiteralActionsAndHeraRail(t *testing.T) {
	d := testDB(t)
	orch := seedHeraOrch(t, d, "orch")
	seedHeraBoundRole(t, d, orch, "coord", db.HeraKindCoordinator, "tc")
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeTaskList
	app.header.SetTab(widget.TabHera)
	app.heraPage.Refresh()
	app.heraPage.Machine().Advance() // rail → coordinator pane

	rows := app.paletteApplicableActions()
	testutil.Equal(t, containsLabel(rows, "toggle fullscreen"), true)
	// resize-without-restart: a focused Hera terminal pane offers the re-render
	// row with its live key (ctrl+n by default).
	for _, r := range rows {
		if r.Label == "re-render at this width" {
			testutil.Equal(t, r.Key, "ctrl+n")
		}
	}
	testutil.Equal(t, containsLabel(rows, "re-render at this width"), true)
	testutil.Equal(t, containsLabel(rows, "quit"), true) // global still unioned in (deliberate pick, not accidental typing)
	testutil.Equal(t, containsLabel(rows, "spawn worker under coordinator (new-task modal)"), true)
	testutil.Equal(t, containsLabel(rows, "new task"), false) // still no cross-tab bleed
}

func TestPaletteApplicableActions_HeraDetailsModeExcludesCopy(t *testing.T) {
	d := testDB(t)
	orch := seedHeraOrch(t, d, "orch")
	// Coordinator-only orchestrator: selecting the header is detailsMode (no
	// worker, so the agent region shows Details/plan, not a terminal).
	seedHeraBoundRole(t, d, orch, "coord", db.HeraKindCoordinator, "tc")
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeTaskList
	app.header.SetTab(widget.TabHera)
	app.heraPage.Refresh()
	app.heraPage.Machine().Advance() // rail → coord
	app.heraPage.Machine().Advance() // coord → agent/details

	rows := app.paletteApplicableActions()
	testutil.Equal(t, containsLabel(rows, "toggle fullscreen"), true)
	testutil.Equal(t, containsLabel(rows, "copy staged clipboard"), false)
}

// --- Smoke tests (real key dispatch through handleGlobalKey/HeraPage) ---

func TestSmoke_CommandPaletteOpensFromFullscreenAgentView(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	seedSwitcherTasks(t, app)
	sim, stop := wireApp(t, app)
	defer stop()

	readUI(t, app.tapp, func() {
		app.agentState.Reset("ts-cur", "current task")
		app.mode = modeAgent
	})

	sim.InjectKey(tcell.KeyCtrlK, 0, 0)
	syncUI(t, app.tapp)

	var mode viewMode
	readUI(t, app.tapp, func() { mode = app.mode })
	testutil.Equal(t, mode, modeCommandPalette)

	sim.InjectKey(tcell.KeyEscape, 0, 0)
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() { mode = app.mode })
	testutil.Equal(t, mode, modeAgent)
}

func TestSmoke_CommandPaletteOpensFromHeraRailAndPane(t *testing.T) {
	d := testDB(t)
	orch := seedHeraOrch(t, d, "orch")
	seedHeraBoundRole(t, d, orch, "coord", db.HeraKindCoordinator, "tc")
	app := New(d, agent.NewRunner(nil), false)
	sim, stop := wireApp(t, app)
	defer stop()

	sim.InjectKey(tcell.KeyRune, '2', 0) // Hera tab, rail focus
	syncUI(t, app.tapp)

	sim.InjectKey(tcell.KeyCtrlK, 0, 0)
	syncUI(t, app.tapp)
	var mode viewMode
	var rowCount int
	readUI(t, app.tapp, func() {
		mode = app.mode
		if app.commandPaletteModal != nil {
			rowCount = len(app.commandPaletteModal.all)
		}
	})
	testutil.Equal(t, mode, modeCommandPalette)
	if rowCount == 0 {
		t.Fatal("expected at least one applicable action from Hera rail focus")
	}

	sim.InjectKey(tcell.KeyEscape, 0, 0)
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() {
		mode = app.mode
		if app.header.ActiveTab() != widget.TabHera {
			t.Fatalf("expected to stay on the Hera tab, got %v", app.header.ActiveTab())
		}
	})
	testutil.Equal(t, mode, modeTaskList)
}

// --- Synthetic-event registries (CtxTaskList/CtxSettings) actually fire the
// real widget dispatch, and go inert during text-input mode ---

// TestTaskListActionRegistry_InvokeFiresRealAction confirms the synthesized
// tcell.EventKey actually reaches TaskListView's own InputHandler (not a
// stub) — invoking ActTaskNew through the registry opens the real new-task
// form, exactly as pressing `n` would.
func TestTaskListActionRegistry_InvokeFiresRealAction(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	app.mode = modeTaskList
	app.header.SetTab(widget.TabTasks)

	reg := app.taskListActionRegistry()
	fn, ok := reg[keymap.ActTaskNew]
	if !ok {
		t.Fatal("expected ActTaskNew to be registered")
	}
	fn()
	testutil.Equal(t, app.mode, modeNewTask)
}

// TestTaskListActionRegistry_NilWhileFiltering guards against a synthesized
// action key silently corrupting the `/` filter query instead of firing the
// action (the registry must go fully inert while filtering).
func TestTaskListActionRegistry_NilWhileFiltering(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	// Enter filter mode via the real InputHandler (the same `/` keypress a
	// user would type) rather than poking private state.
	app.tasklist.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone), func(tview.Primitive) {})
	testutil.Equal(t, app.tasklist.Filtering(), true)

	if reg := app.taskListActionRegistry(); reg != nil {
		t.Fatal("expected a nil registry while the task list is filtering")
	}
}

// TestSettingsActionRegistry_InvokeFiresRealAction mirrors the task-list
// case for SettingsView.HandleKey: invoking ActSettingsQuickAdd through the
// registry fires the real OnQuickAdd callback (category gated, exactly as a
// physical `i` keypress would be).
func TestSettingsActionRegistry_InvokeFiresRealAction(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	app.settings.category = catProjects
	fired := false
	app.settings.OnQuickAdd = func() { fired = true }

	reg := app.settingsActionRegistry()
	fn, ok := reg[keymap.ActSettingsQuickAdd]
	if !ok {
		t.Fatal("expected ActSettingsQuickAdd to be registered")
	}
	fn()
	testutil.Equal(t, fired, true)
}

// TestSettingsActionRegistry_NilWhileEditing mirrors the task-list filtering
// guard: a synthesized action key must not reach an in-progress inline edit.
func TestSettingsActionRegistry_NilWhileEditing(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	app.settings.editingBackendModel = "some-backend"
	testutil.Equal(t, app.settings.IsEditing(), true)

	if reg := app.settingsActionRegistry(); reg != nil {
		t.Fatal("expected a nil registry while a settings field is being edited")
	}
}

// TestSmoke_CommandPaletteOpensFromPlainTaskList is the real end-to-end
// dispatch regression the unit-level TestPaletteApplicableActions_* tests
// didn't cover: driving ctrl+k through the actual handleGlobalKey SetInputCapture
// path from the plain Tasks tab (no Hera, no agent view) — the exact gap a
// live QA pass found dead.
func TestSmoke_CommandPaletteOpensFromPlainTaskList(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	seedSwitcherTasks(t, app)
	sim, stop := wireApp(t, app)
	defer stop()

	readUI(t, app.tapp, func() {
		if app.mode != modeTaskList || app.header.ActiveTab() != widget.TabTasks {
			t.Fatalf("setup: expected modeTaskList/TabTasks, got mode=%v tab=%v", app.mode, app.header.ActiveTab())
		}
	})

	sim.InjectKey(tcell.KeyCtrlK, 0, 0)
	syncUI(t, app.tapp)

	var mode viewMode
	var rowCount int
	readUI(t, app.tapp, func() {
		mode = app.mode
		if app.commandPaletteModal != nil {
			rowCount = len(app.commandPaletteModal.all)
		}
	})
	testutil.Equal(t, mode, modeCommandPalette)
	if rowCount == 0 {
		t.Fatal("expected at least one applicable action from the plain Tasks tab")
	}

	sim.InjectKey(tcell.KeyEscape, 0, 0)
	syncUI(t, app.tapp)
	readUI(t, app.tapp, func() { mode = app.mode })
	testutil.Equal(t, mode, modeTaskList)
}

func labelsOf(rows []paletteRow) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Label] = true
	}
	return out
}

// The resident tab's palette is the trimmed global set plus ONLY the rail
// actions the keymap's resident policy allows: inbox on the resident row, and
// nuke/hide/rename/status only once a worker row is selected. Creation and
// structure actions never appear.
func TestPaletteApplicableActions_ResidentTabFollowsKeymapPolicy(t *testing.T) {
	a, d := residentTestApp(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "w1", Name: "w1", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))
	orchs, err := d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	_, _, err = d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orchs[0].ID, Name: "w1", Kind: db.HeraKindWorker, ArgusProject: "p"}, "w1", t.TempDir())
	testutil.NoError(t, err)
	a.switchTab(widget.TabResident)
	a.heraPage.Machine().ToRail()
	km := a.activeKeymap()
	label := km.ActionLabel

	got := labelsOf(a.paletteApplicableActions())
	for _, act := range []keymap.Action{keymap.ActGlobalResident, keymap.ActGlobalSwitcher, keymap.ActGlobalTabTasks, keymap.ActGlobalTabHera, keymap.ActGlobalTabSettings} {
		testutil.True(t, got[label(act)])
	}
	// Resident row selected: only the inbox among rail actions.
	testutil.True(t, got[label(keymap.ActHeraInbox)])
	for _, act := range []keymap.Action{keymap.ActHeraDelete, keymap.ActHeraArchive, keymap.ActHeraRename, keymap.ActHeraStatAdv, keymap.ActHeraStatRev} {
		testutil.False(t, got[label(act)])
	}
	// Never offered on this tab, whatever the selection.
	inert := []keymap.Action{keymap.ActHeraSpawn, keymap.ActHeraNewCoord, keymap.ActHeraPin, keymap.ActHeraKanbanAdv,
		keymap.ActHeraKanbanRev, keymap.ActHeraAdopt, keymap.ActHeraClear, keymap.ActHeraCleanup, keymap.ActHeraMoveAccount}
	for _, act := range inert {
		testutil.False(t, got[label(act)])
	}

	// Worker row selected: the worker-only management actions appear.
	testutil.True(t, a.heraPage.JumpToTask("w1"))
	a.heraPage.Machine().ToRail()
	got = labelsOf(a.paletteApplicableActions())
	for _, act := range []keymap.Action{keymap.ActHeraDelete, keymap.ActHeraArchive, keymap.ActHeraRename, keymap.ActHeraStatAdv, keymap.ActHeraStatRev, keymap.ActHeraInbox} {
		testutil.True(t, got[label(act)])
	}
	for _, act := range inert {
		testutil.False(t, got[label(act)])
	}
}

func TestPaletteApplicableActions_AgentViewResidentRowFollowsEnabled(t *testing.T) {
	a, d := residentTestApp(t)
	a.mode = modeAgent
	label := a.activeKeymap().ActionLabel(keymap.ActGlobalResident)
	testutil.True(t, containsLabel(a.paletteApplicableActions(), label))
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "false"))
	a.refreshResidentTab()
	testutil.False(t, containsLabel(a.paletteApplicableActions(), label))
}
