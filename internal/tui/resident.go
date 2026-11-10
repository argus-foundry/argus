package tui

import (
	"time"

	heramodel "github.com/argus-foundry/argus/internal/hera/model"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/argus-foundry/argus/internal/uxlog"
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
// The resident is local-only until add-resident-web exposes a REST surface. It
// only maintains the header label and bounces a viewer back to Tasks when the
// resident is disabled: the resident session itself is bound by the shared
// HeraPage in resident mode (enterResidentView), never by this function.
func (a *App) refreshResidentTab() {
	_, local := a.db.(*db.DB)
	cfg := a.db.Config().Resident
	label := residentTabLabel(local, cfg)
	// The marker is folded into this single label write so the tick can never
	// clobber it; the stored attention state is applied by applyResidentState.
	a.header.SetTabLabel(widget.TabResident, a.residentLabelWithMarker(label))
	if a.header.ActiveTab() != widget.TabResident {
		return
	}
	if label == "" {
		uxlog.Log("[resident] disabled while viewed; returning to Tasks")
		a.switchTab(widget.TabTasks)
	}
}

// enterResidentView points the shared HeraPage at the standing team and shows
// it: resident row = the resident conversation pane, worker rows = the worker
// pane, `i` = the resident's inbox. The tab always opens with the rail focused.
// UI goroutine only.
func (a *App) enterResidentView() {
	a.heraPage.SetResidentMode(true)
	if !a.heraPage.ResidentMode() {
		// No resident reader (remote/bare page): never show the Projects view
		// under the resident tab's name.
		uxlog.Log("[resident] resident view unavailable; returning to Tasks")
		a.switchTab(widget.TabTasks)
		return
	}
	// Open on the resident conversation (typing goes straight to its PTY, as
	// the tab always did); ctrl+alt+← reaches the team rail.
	a.heraPage.Machine().SetRegion(hera.FocusCoord)
	a.statusbar.SetHeraFocus(int(hera.FocusCoord))
	a.heraPage.SetResidentTitle(a.db.Config().Resident.Name)
	a.heraPage.CoordPane().SetPlaceholderNote(a.residentSt.reason)
	a.pages.SwitchToPage("hera")
	a.tapp.SetFocus(a.heraPage)
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
	case widget.TabHera, widget.TabResident:
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
		// Return directly to the origin: restoreResidentReturn makes the single
		// switchTab to the target, so there is no detour through Tasks (which
		// would persist/sync twice and flash the wrong tab).
		ret := a.residentReturn
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

// restoreResidentReturn leaves the resident tab for the recorded origin with
// one switchTab. False means the target vanished (nothing was switched for an
// agent-view target; a tab target may already be showing) and the caller falls
// back to Tasks.
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
	reason          string // daemon's "why isn't it running"; empty when healthy
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

// pollResidentUnread refreshes the resident view's unread-mail counts (the
// per-worker "✉N" rail tags and the resident pane title) while that view is
// showing. The role list is snapshotted on the UI goroutine; the per-role
// queries run on one single-flight goroutine and the result is applied via
// QueueUpdateDraw, never in Draw. Counts are Hera inbox plus task messages.
func (a *App) pollResidentUnread() {
	if a.header.ActiveTab() != widget.TabResident || !a.heraPage.ResidentMode() ||
		a.residentUnreadPending || time.Since(a.residentUnreadAt) < residentPollInterval {
		return
	}
	d, ok := a.db.(*db.DB)
	if !ok {
		return
	}
	type ref struct {
		roleID int64
		taskID string
	}
	var refs []ref
	m := a.heraPage.Rail().Model()
	m.AnnotateRoles(func(rv *heramodel.RoleView) {
		if rv.TaskID != "" && rv.RoleID != 0 {
			refs = append(refs, ref{rv.RoleID, rv.TaskID})
		}
	})
	a.residentUnreadPending = true
	a.residentUnreadAt = time.Now()
	go func() {
		counts := map[string]int{}
		for _, r := range refs {
			n := 0
			if msgs, err := d.HeraInbox(r.roleID); err == nil {
				n += len(msgs)
			}
			if c, err := d.UnreadCount(r.taskID); err == nil {
				n += c
			}
			if n > 0 {
				counts[r.taskID] = n
			}
		}
		a.tapp.QueueUpdateDraw(func() {
			a.residentUnreadPending = false
			if !a.heraPage.ResidentMode() {
				uxlog.Log("[resident] unread poll result dropped: view left")
				return
			}
			a.heraPage.SetUnread(counts)
			if len(counts) > 0 {
				uxlog.Log("[resident] unread counts roles=%d", len(counts))
			}
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
	// The daemon reports prerequisite reasons even while the session runs; only
	// a not-running resident has anything to explain.
	reason := ""
	if !resp.Running {
		reason = sanitizeResidentReason(resp.Reason)
	}
	next := residentAttention{unread: unread, waiting: resp.Waiting, preview: resp.LastReply,
		reason: reason}
	if next != a.residentSt {
		uxlog.Log("[resident] attention state unread=%v waiting=%v reason=%q", next.unread, next.waiting, next.reason)
	}
	a.residentSt = next
	a.settings.SetResidentReason(next.reason)
	if a.heraPage.ResidentMode() {
		a.heraPage.CoordPane().SetPlaceholderNote(next.reason)
	}
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
	_, local := a.db.(*db.DB)
	label := residentTabLabel(local, a.db.Config().Resident)
	a.header.SetTabLabel(widget.TabResident, a.residentLabelWithMarker(label))
}

// residentTabLabel is the single rule for the resident tab's label: shown only
// locally, when enabled, and when the name is valid — an invalid name (too
// long, control bytes) must never reach the header.
func residentTabLabel(local bool, cfg config.ResidentConfig) string {
	if local && cfg.Enabled && config.ValidResidentName(cfg.Name) == nil {
		return cfg.Name
	}
	return ""
}
