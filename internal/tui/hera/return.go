package hera

// ReturnSnapshot reuses the rail excursion snapshot without taking ownership of
// an outstanding ctrl+g excursion. Pane focus and zoom belong to this page.
type ReturnSnapshot struct {
	rail       *railSnapshot
	focus      Focus
	fullscreen bool
	cursor     int
}

// CaptureReturn records the current selection, folds, focus and zoom.
func (p *HeraPage) CaptureReturn() *ReturnSnapshot {
	return &ReturnSnapshot{rail: p.rail.captureExcursionSnapshot(), focus: p.focus.State(), fullscreen: p.focus.Fullscreen(), cursor: p.rail.cursor}
}

// RestoreReturn reapplies a captured view. False means the selected target no
// longer exists; the caller should fall back to its Tasks list.
func (p *HeraPage) RestoreReturn(s *ReturnSnapshot) bool {
	if s == nil || s.rail == nil {
		return false
	}
	r, snap := p.rail, s.rail
	r.collapsed = cloneInt64BoolMap(snap.collapsed)
	r.coordArchiveOpen = cloneInt64BoolMap(snap.coordArchiveOpen)
	r.freelanceCollap = snap.freelanceCollap
	r.archiveCollapsed = snap.archiveCollapsed
	r.focusedKanban = snap.focusedKanban
	r.buildRows()
	if snap.selRef == 0 {
		r.cursor = s.cursor
	} else {
		r.restoreCursor(snap.selRef)
		if r.currentRef() != snap.selRef {
			return false
		}
	}
	r.clampCursor()
	p.applySelection()
	p.focus.SetRegion(s.focus)
	if p.focus.Fullscreen() != s.fullscreen {
		p.focus.ToggleFullscreen()
	}
	if p.OnFocusChange != nil {
		p.OnFocusChange(p.focus.State())
	}
	return p.focus.State() == s.focus
}
