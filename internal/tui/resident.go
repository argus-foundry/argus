package tui

import (
	"github.com/drn/argus/internal/app/agentview"
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
	if local && cfg.Enabled {
		label = cfg.Name
	}
	a.header.SetTabLabel(widget.TabResident, label)
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
	if a.mode != modeTaskList && a.mode != modeAgent {
		return true
	}
	if a.mode == modeAgent {
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
	if c := a.daemonClient; c != nil {
		go func() {
			if err := c.ResidentMarkViewed(); err != nil {
				uxlog.Log("[resident] mark viewed failed: %v", err)
			} else {
				uxlog.Log("[resident] marked viewed")
			}
		}()
	}
}
