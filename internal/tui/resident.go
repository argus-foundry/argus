package tui

import (
	"time"

	"github.com/drn/argus/internal/app/agentview"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/daemon"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/tui/hera"
	"github.com/drn/argus/internal/tui/widget"
	"github.com/drn/argus/internal/uxlog"
	"github.com/gdamore/tcell/v2"
)

// residentReturn generalizes the agent-view cross-tab hop. Snapshots are UI
// goroutine owned and recorded AFTER switchTab, which clears prior hops.
type residentReturn struct {
	tab            widget.Tab
	mode           viewMode
	taskID         string
	zen            bool
	agentFocus     agentFocus
	hera           *hera.ReturnSnapshot
	category       settingsCategory
	plugin         pluginKey
	settingsFocus  settingsFocus
	cursor, scroll int
	crossReturn    *residentReturn
	listTaskID     string
	railViewing    bool
}

// refreshResidentTab runs on the UI goroutine alongside keymap/config refresh.
// The resident is local-only until add-resident-web exposes a REST surface.
func (a *App) refreshResidentTab() {
	d, local := a.db.(*db.DB)
	cfg := a.db.Config().Resident
	label := ""
	// An invalid name (too long, control bytes) must never reach the header.
	if local && cfg.Enabled && config.ValidResidentName(cfg.Name) == nil {
		label = cfg.Name
	}
	// The marker is folded into this single label write so the tick can never
	// clobber it; the stored attention state is applied by applyResidentState.
	a.header.SetTabLabel(widget.TabResident, a.residentLabelWithMarker(label))
	if a.header.ActiveTab() != widget.TabResident {
		return
	}
	if label == "" {
		uxlog.Log("[resident] disabled while viewed; returning to Tasks")
		a.switchTab(widget.TabTasks)
		return
	}
	row, err := d.ResidentTask()
	if err != nil {
		uxlog.Log("[resident] task lookup failed: %v", err)
		return
	}
	if row == nil {
		uxlog.Log("[resident] session attachment skipped: singleton not created yet")
		return // daemon creates the singleton; the TUI never does
	}
	if a.residentPane.TaskID() != row.ID {
		a.residentPane.SetTaskID(row.ID)
		a.residentPane.ResetVT()
	}
	a.residentPane.SetBorderTitle(cfg.Name)
	if a.daemonClient == nil {
		a.residentPane.SetSession(a.runner.Get(row.ID))
		return
	}
	if a.residentAttachPending {
		return
	}
	a.residentAttachPending = true
	runner, taskID := a.runner, row.ID
	go func() {
		sess := runner.Get(taskID) // can probe the daemon; never on the UI goroutine
		a.tapp.QueueUpdateDraw(func() {
			a.residentAttachPending = false
			if a.header.ActiveTab() != widget.TabResident || a.residentPane.TaskID() != taskID || a.runner != runner {
				uxlog.Log("[resident] skipped stale session attachment task=%s", taskID)
				return
			}
			a.residentPane.SetSession(sess)
			uxlog.Log("[resident] session attachment task=%s available=%v", taskID, sess != nil)
		})
	}()
}

func (a *App) forwardResidentKey(event *tcell.EventKey) {
	// Match the agent and Hera SIGTSTP safety net.
	if event.Key() == tcell.KeyCtrlZ {
		return
	}
	sess := a.residentPane.Session()
	if sess == nil || !sess.Alive() {
		return
	}
	if b := tcellKeyToBytes(event); len(b) > 0 {
		a.residentPane.ResetScroll()
		if _, err := sess.WriteInput(b, agentview.OriginUser); err != nil {
			uxlog.Log("[resident] PTY input failed: %v", err)
		}
	}
}

func (a *App) residentInputBlocked() bool {
	return a.residentModalBlocked() || a.residentTextInputBlocked()
}

// residentModalBlocked reports a modal or non-list mode owning the keyboard.
func (a *App) residentModalBlocked() bool {
	return a.mode != modeTaskList && a.mode != modeAgent
}

// residentTextInputBlocked reports a focused text field on the list modes.
func (a *App) residentTextInputBlocked() bool {
	if a.mode != modeTaskList {
		return false
	}
	switch a.header.ActiveTab() {
	case widget.TabTasks:
		return a.tasklist.Filtering()
	case widget.TabSettings:
		return a.settings.IsEditing()
	case widget.TabHera:
		return a.heraPage.RailFiltering()
	default:
		return false
	}
}

func (a *App) toggleResident() {
	if a.residentInputBlocked() {
		uxlog.Log("[resident] toggle skipped: modal or text input")
		return
	}
	if a.header.ActiveTab() == widget.TabResident {
		ret := a.residentReturn
		a.switchTab(widget.TabTasks)
		if ret == nil || !a.restoreResidentReturn(ret) {
			uxlog.Log("[resident] return target missing; using Tasks")
			a.switchTab(widget.TabTasks)
		}
		return
	}
	a.refreshResidentTab()
	if a.header.TabLabel(widget.TabResident) == "" {
		uxlog.Log("[resident] toggle skipped: disabled or unavailable")
		return
	}
	ret := &residentReturn{tab: a.header.ActiveTab(), mode: a.mode, taskID: a.agentState.TaskID,
		zen: a.agentZen, agentFocus: a.agentFocus, crossReturn: a.agentCrossReturn,
		category: a.settings.category, plugin: a.settings.activePlugin, settingsFocus: a.settings.focus,
		cursor: a.settings.cursor, scroll: a.settings.scrollOff, railViewing: a.tasksRailViewing}
	if task := a.tasklist.SelectedTask(); task != nil {
		ret.listTaskID = task.ID
	}
	if ret.tab == widget.TabHera {
		ret.hera = a.heraPage.CaptureReturn()
	}
	a.switchTab(widget.TabResident)
	a.residentReturn = ret
	uxlog.Log("[resident] opened from tab=%d mode=%d task=%s", ret.tab, ret.mode, ret.taskID)
}

func (a *App) restoreResidentReturn(ret *residentReturn) bool {
	if ret.mode == modeAgent || ret.railViewing {
		task, err := a.db.Get(ret.taskID)
		if err != nil || task == nil {
			return false
		}
		a.switchTab(widget.TabTasks)
		a.bindTaskAgent(task, false, ret.mode == modeAgent)
		if ret.zen {
			a.setAgentZen()
		} else {
			a.clearAgentZen()
		}
		a.agentFocus = ret.agentFocus
		a.updateFocusIndicators()
		if ret.railViewing && ret.mode == modeTaskList {
			a.focusTasksRail()
		}
	} else {
		a.switchTab(ret.tab)
		switch ret.tab {
		case widget.TabHera:
			if !a.heraPage.RestoreReturn(ret.hera) {
				return false
			}
		case widget.TabSettings:
			a.settings.activePlugin = ret.plugin
			a.settings.setCategory(ret.category)
			if ret.category == catPlugin && a.settings.activePluginSection() == nil {
				return false
			}
			a.settings.setFocus(ret.settingsFocus)
			a.settings.cursor, a.settings.scrollOff = ret.cursor, ret.scroll
		case widget.TabTasks:
			if ret.listTaskID != "" {
				a.tasklist.SelectByID(ret.listTaskID)
			}
		}
	}
	a.agentCrossReturn = ret.crossReturn
	uxlog.Log("[resident] restored tab=%d mode=%d task=%s", ret.tab, ret.mode, ret.taskID)
	return true
}

func (a *App) markResidentViewed() {
	fn := a.residentMarkViewedFn
	if fn == nil {
		c := a.daemonClient
		if c == nil {
			return
		}
		fn = c.ResidentMarkViewed
	}
	go func() {
		if err := fn(); err != nil {
			uxlog.Log("[resident] mark viewed failed: %v", err)
		} else {
			uxlog.Log("[resident] marked viewed")
		}
	}()
}

// residentPollInterval bounds how often the UI tick asks the daemon for the
// resident's unread/waiting state.
const residentPollInterval = 2 * time.Second

// residentAttention is the UI-goroutine-owned copy of the daemon state that
// drives the tab-label marker and the rail box.
type residentAttention struct {
	unread, waiting bool
	preview         string
}

// residentLabelWithMarker appends the marker to the tab label. The marker is
// withheld while the resident tab itself is showing (viewing clears it) and
// when the label is empty (disabled hides everything).
func (a *App) residentLabelWithMarker(label string) string {
	if label == "" || a.header.ActiveTab() == widget.TabResident {
		return label
	}
	if m := widget.ResidentMarker(a.residentSt.unread, a.residentSt.waiting); m != "" {
		return label + " " + m
	}
	return label
}

// pollResidentState runs on the UI tick: it fetches the daemon state on a
// background goroutine (single flight, never on the UI goroutine or in Draw)
// and delivers via QueueUpdateDraw.
func (a *App) pollResidentState() {
	fn := a.residentStateFn
	if fn == nil {
		c := a.daemonClient
		if c == nil {
			return
		}
		fn = c.ResidentState
	}
	if a.residentPollPending || a.header.TabLabel(widget.TabResident) == "" ||
		time.Since(a.residentPolledAt) < residentPollInterval {
		return
	}
	a.residentPollPending = true
	a.residentPolledAt = time.Now()
	go func() {
		resp, err := fn()
		a.tapp.QueueUpdateDraw(func() {
			a.residentPollPending = false
			if err != nil {
				uxlog.Log("[resident] state poll failed: %v", err)
				return
			}
			a.applyResidentState(resp)
		})
	}()
}

// applyResidentState stores the daemon state and refreshes the box and label.
// While the resident tab is focused an unread reply is acked immediately so a
// reply arriving mid-watch never leaves a stale marker.
func (a *App) applyResidentState(resp daemon.ResidentStateResp) {
	viewing := a.header.ActiveTab() == widget.TabResident
	unread := resp.Unread
	if viewing && unread {
		uxlog.Log("[resident] reply arrived while viewing; acking")
		a.markResidentViewed()
		unread = false
	}
	next := residentAttention{unread: unread, waiting: resp.Waiting, preview: resp.LastReply}
	if next != a.residentSt {
		uxlog.Log("[resident] attention state unread=%v waiting=%v", next.unread, next.waiting)
	}
	a.residentSt = next
	a.refreshResidentTab()
	a.syncResidentBox()
}

// syncResidentBox pushes the stored state into the rail box, suppressed while
// the resident tab is showing or the resident is disabled.
func (a *App) syncResidentBox() {
	name := a.header.TabLabel(widget.TabResident)
	suppressed := a.header.ActiveTab() == widget.TabResident
	if name == "" {
		suppressed = true
	} else {
		name = a.db.Config().Resident.Name
	}
	st := a.residentSt
	a.residentBox.Set(name, st.unread, st.waiting, st.preview, suppressed)
}

// refreshResidentLabelOnly re-derives just the header label (marker) from the
// current config after a tab change, without touching the pane attachment.
func (a *App) refreshResidentLabelOnly() {
	label := ""
	if _, local := a.db.(*db.DB); local && a.db.Config().Resident.Enabled {
		label = a.db.Config().Resident.Name
	}
	a.header.SetTabLabel(widget.TabResident, a.residentLabelWithMarker(label))
}
