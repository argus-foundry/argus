package taskview

import "github.com/argus-foundry/argus/internal/uxlog"

// ExpandAll reports the remembered project expansion preference.
func (tl *TaskListView) ExpandAll() bool { return tl.expandAll }

func (tl *TaskListView) SetExpandAll(enabled bool) {
	if tl.expandAll == enabled {
		return
	}
	var prev taskRow
	hasPrev := tl.cursor >= 0 && tl.cursor < len(tl.rows)
	sec := sectionActive
	if hasPrev {
		prev = tl.rows[tl.cursor]
		sec = tl.sectionAt(tl.cursor)
	}
	tl.expandAll = enabled
	if hasPrev && prev.project != "" {
		if sec == sectionActive {
			tl.expanded = prev.project
		}
		if sec == sectionArchive {
			tl.archiveProject = prev.project
		}
	}
	tl.buildRows()
	if hasPrev {
		tl.restoreCursor(prev, sec)
	}
	tl.clampCursor()
	uxlog.Log("[tasks-rail] expand all=%v", enabled)
}
