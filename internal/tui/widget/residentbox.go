package widget

import (
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Resident attention markers. Waiting outranks unread.
const (
	ResidentMarkerUnread  = "●"
	ResidentMarkerWaiting = "?"
)

// ResidentMarker returns the marker for a resident state: "?" when waiting,
// "●" when unread, "" when quiet. Waiting wins.
func ResidentMarker(unread, waiting bool) string {
	switch {
	case waiting:
		return ResidentMarkerWaiting
	case unread:
		return ResidentMarkerUnread
	}
	return ""
}

// ResidentBox is the one-row resident attention box stacked beside the
// needs-input box. It reports zero desired height when the resident is quiet
// (read and not waiting) or suppressed (the resident tab itself is showing).
type ResidentBox struct {
	*tview.Box
	name, preview   string
	unread, waiting bool
	suppressed      bool
	lastHeight      int
	// OnHeightChange fires when DesiredHeight changes so the parent flex can
	// resize only the left column. It never touches any PTY pane.
	OnHeightChange func()
}

// NewResidentBox creates a quiet (hidden) box.
func NewResidentBox() *ResidentBox { return &ResidentBox{Box: tview.NewBox()} }

// Set replaces the displayed state and fires OnHeightChange on a height edge.
func (b *ResidentBox) Set(name string, unread, waiting bool, preview string, suppressed bool) {
	b.name, b.unread, b.waiting, b.preview, b.suppressed = name, unread, waiting, preview, suppressed
	if h := b.DesiredHeight(); h != b.lastHeight {
		b.lastHeight = h
		if b.OnHeightChange != nil {
			b.OnHeightChange()
		}
	}
}

// Marker returns the box's current marker (empty when quiet).
func (b *ResidentBox) Marker() string { return ResidentMarker(b.unread, b.waiting) }

// DesiredHeight is 1 when a marker is showing and the box is not suppressed.
func (b *ResidentBox) DesiredHeight() int {
	if b.suppressed || b.name == "" || b.Marker() == "" {
		return 0
	}
	return 1
}

// Draw paints "<marker> <name> <preview…>" on one row.
func (b *ResidentBox) Draw(screen tcell.Screen) {
	b.DrawForSubclass(screen, b)
	x, y, w, h := b.GetInnerRect()
	if w <= 0 || h <= 0 || b.DesiredHeight() == 0 {
		return
	}
	FillArea(screen, x, y, w, 1, ' ', theme.StyleDefault)
	style := theme.StyleInReview
	if b.waiting {
		style = theme.StyleNeedsInput
	}
	line := b.Marker() + " " + b.name
	if b.preview != "" {
		line += ": " + b.preview
	}
	DrawText(screen, x, y, w, truncateAttention(line, w), style)
}
