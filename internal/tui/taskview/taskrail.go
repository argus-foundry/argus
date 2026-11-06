package taskview

import (
	"github.com/drn/argus/internal/tui/theme"
	"github.com/drn/argus/internal/uxlog"
	"github.com/gdamore/tcell/v2"
)

// SetRailMode opts into the persistent task rail and its Archive expansion state.
func (tl *TaskListView) SetRailMode(enabled bool) {
	if tl.railMode == enabled {
		return
	}
	id := ""
	if task := tl.SelectedTask(); task != nil {
		id = task.ID
	}
	tl.railMode = enabled
	tl.buildRows()
	if id != "" {
		tl.selectByID(id)
	} else {
		tl.clampCursor()
	}
	uxlog.Log("[tasks-rail] list mode enabled=%v", enabled)
}

func (tl *TaskListView) selectable(idx int) bool {
	if idx < 0 || idx >= len(tl.rows) {
		return false
	}
	return tl.rows[idx].kind == rowTask
}

func (tl *TaskListView) drawRailCursor(screen tcell.Screen, x, y int) {
	style := theme.StyleSelected
	if tl.cursorDimmed {
		style = theme.StyleDimmed
	}
	screen.SetContent(x, y, '›', nil, style)
}
