package widget

import (
	"fmt"
	"time"

	"github.com/drn/argus/internal/spinner"
	"github.com/drn/argus/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

// Tab identifies the active top-level tab.
type Tab int

const (
	TabTasks Tab = iota
	// TabHera is the second tab slot. It was historically TabDAG; M6 relabels
	// it to the native Hera view (the DAG renderer folds into the Hera view's
	// details pane in M7). The enum slot is kept so the `2` hotkey is stable.
	TabHera
	TabSettings
	// TabResident is rendered first when enabled, without renumbering 1/2/3.
	TabResident
)

var TabLabels = [...]string{"Tasks", "Projects", "Settings"}
var tabKeys = [...]string{"1", "2", "3"}

// Powerline separator (right-facing filled chevron).
const powerlineSep = '\ue0b0'

// HeaderNoticeTTL is how long a notice set via SetNotice stays visible before
// auto-clearing, mirroring StatusBar's expiresAt/tick model (BUG-030). The
// clear is realized lazily in Draw/Notice \u2014 the app's unconditional ~1s
// onTick QueueUpdateDraw repaints the header within a tick of expiry, so no
// notice can linger forever on a static screen. Never force it with a
// timer/goroutine (see gotchas/ui-threading.md).
const HeaderNoticeTTL = 2 * time.Second

// Colors matching the tmux status bar palette.
var (
	headerBaseBG     = tcell.Color236 // dark bar background (C3)
	headerActiveBG   = tcell.Color103 // lavender — active segment (C1)
	headerActiveFG   = tcell.Color236 // dark text on active
	headerInactiveBG = tcell.Color239 // medium gray — inactive segment (C2)
	headerInactiveFG = tcell.Color253 // light text on inactive
)

// Header renders the top tab bar with tmux-style powerline separators.
// It also supports a general-purpose notice area on the left side
// (spinner + short text) for async background operations.
type Header struct {
	*tview.Box
	activeTab Tab

	// tabLabels holds the per-slot display text. Initialized from TabLabels;
	// individual slots can be overridden via SetTabLabel. Width computations and
	// Draw both consume this field so the label and centering always agree.
	tabLabels     [len(TabLabels)]string
	residentLabel string // empty hides the optional first tab

	// Notice: general-purpose status indicator (left side of header).
	// Auto-expires after HeaderNoticeTTL (BUG-030-style); see expireNotice.
	noticeText      string // empty = no notice
	noticeExpiresAt time.Time

	// now is the widget's time source; injectable so tests exercise expiry
	// without a real sleep. Defaults to time.Now via NewHeader; falls back to
	// time.Now in clock() for zero-value literals (direct-construction tests).
	now func() time.Time
}

// NewHeader creates a tab bar header.
func NewHeader() *Header {
	h := &Header{
		Box:       tview.NewBox(),
		activeTab: TabTasks,
		now:       time.Now,
	}
	h.tabLabels = TabLabels
	return h
}

// clock returns the widget's time source, falling back to time.Now if the
// Header was built without NewHeader (e.g. a zero-value literal).
func (h *Header) clock() time.Time {
	if h.now == nil {
		return time.Now()
	}
	return h.now()
}

// expireNotice clears the notice once its TTL has elapsed. Called on every
// read path (Draw, Notice) so a stale notice is dropped the moment a repaint
// or accessor observes it past expiry — no timer/goroutine needed.
func (h *Header) expireNotice() {
	if h.noticeText != "" && !h.noticeExpiresAt.IsZero() && !h.clock().Before(h.noticeExpiresAt) {
		h.noticeText = ""
		h.noticeExpiresAt = time.Time{}
	}
}

// SetTabLabel overrides the display text for a single tab slot. Call before or
// after construction; takes effect on the next Draw.
func (h *Header) SetTabLabel(t Tab, label string) {
	if t == TabResident {
		h.residentLabel = label
		return
	}
	if idx := int(t); idx >= 0 && idx < len(h.tabLabels) {
		h.tabLabels[idx] = label
	}
}

// TabLabel returns the current display text for tab slot t.
func (h *Header) TabLabel(t Tab) string {
	if t == TabResident {
		return h.residentLabel
	}
	if idx := int(t); idx >= 0 && idx < len(h.tabLabels) {
		return h.tabLabels[idx]
	}
	return ""
}

// SetTab changes the active tab.
func (h *Header) SetTab(t Tab) {
	h.activeTab = t
}

// ActiveTab returns the current tab.
func (h *Header) ActiveTab() Tab {
	return h.activeTab
}

// SetNotice sets the notice text displayed with a spinner on the left. It
// auto-clears after HeaderNoticeTTL; each call resets that window so a fresh
// notice always gets its full TTL.
func (h *Header) SetNotice(text string) {
	h.noticeText = text
	h.noticeExpiresAt = h.clock().Add(HeaderNoticeTTL)
}

// ClearNotice removes the notice.
func (h *Header) ClearNotice() {
	h.noticeText = ""
	h.noticeExpiresAt = time.Time{}
}

// Notice returns the current notice text ("" if none or expired).
func (h *Header) Notice() string {
	h.expireNotice()
	return h.noticeText
}

// Draw renders the tab bar with powerline-style segments, centered.
// If a notice is active, a spinner + text is drawn on the left.
func (h *Header) Draw(screen tcell.Screen) {
	h.expireNotice()
	h.Box.DrawForSubclass(screen, h)
	x, y, width, _ := h.GetInnerRect()
	if width <= 0 {
		return
	}

	// Fill entire row with base background
	baseStyle := tcell.StyleDefault.Background(headerBaseBG)
	for i := 0; i < width; i++ {
		screen.SetContent(x+i, y, ' ', nil, baseStyle)
	}

	// Draw notice on the left (spinner + text) if active.
	noticeEnd := x
	if h.noticeText != "" {
		sp := spinner.Get(spinner.StyleProgress)
		frame := int(time.Now().UnixMilli()/sp.TickInterval.Milliseconds()) % sp.FrameCount()
		spinnerRune := sp.Frame(frame)

		noticeStyle := tcell.StyleDefault.Background(headerBaseBG).Foreground(theme.ColorInProgress)
		textStyle := tcell.StyleDefault.Background(headerBaseBG).Foreground(theme.ColorNormal)

		col := x + 1 // left padding
		screen.SetContent(col, y, spinnerRune, nil, noticeStyle)
		col += 2 // advance past spinner + gap
		for _, r := range h.noticeText {
			if col >= x+width {
				break
			}
			screen.SetContent(col, y, r, nil, textStyle)
			col++
		}
		noticeEnd = col + 1 // gap between notice and tab segments
	}

	// Compute total width of all tab segments to center them.
	// Each segment = 1 (open chevron) + len(text) + 1 (close chevron).
	totalWidth := 0
	tabs := []Tab{TabTasks, TabHera, TabSettings}
	if h.residentLabel != "" {
		tabs = append([]Tab{TabResident}, tabs...)
	}
	for _, tab := range tabs {
		label := h.TabLabel(tab)
		text := fmt.Sprintf(" %s ", label)
		totalWidth += 1 + textCells(text) + 1 // open sep + text cells + close sep
	}

	col := x + (width-totalWidth)/2
	if col < noticeEnd {
		col = noticeEnd
	}

	// Draw tabs
	for _, tab := range tabs {
		label := h.TabLabel(tab)
		if col >= x+width {
			break
		}
		text := fmt.Sprintf(" %s ", label)
		if tab == h.activeTab {
			col = h.drawSegment(screen, col, y, x+width, text, headerActiveBG, headerActiveFG, true)
		} else {
			col = h.drawSegment(screen, col, y, x+width, text, headerInactiveBG, headerInactiveFG, false)
		}
	}
}

// runeCells is the terminal cell advance for r: wide runes (the resident
// marker) take two, zero-width runes still advance one so width math agrees.
func runeCells(r rune) int {
	if w := runewidth.RuneWidth(r); w > 1 {
		return w
	}
	return 1
}

// textCells sums runeCells over text; it must match drawSegment's advance.
func textCells(text string) int {
	n := 0
	for _, r := range text {
		n += runeCells(r)
	}
	return n
}

// drawSegment renders a powerline-style segment: opening chevron, text, closing chevron.
// Returns the column position after the segment.
func (h *Header) drawSegment(screen tcell.Screen, col, y, maxCol int, text string, bg, fg tcell.Color, bold bool) int {
	if col >= maxCol {
		return col
	}

	// Opening separator: transition from base → segment
	sepStyle := tcell.StyleDefault.Foreground(headerBaseBG).Background(bg)
	screen.SetContent(col, y, powerlineSep, nil, sepStyle)
	col++

	// Text
	textStyle := tcell.StyleDefault.Foreground(fg).Background(bg)
	if bold {
		textStyle = textStyle.Bold(true)
	}
	for _, r := range text {
		if col >= maxCol {
			return col
		}
		screen.SetContent(col, y, r, nil, textStyle)
		col += runeCells(r)
	}

	// Closing separator: transition from segment → base
	if col < maxCol {
		sepStyle = tcell.StyleDefault.Foreground(bg).Background(headerBaseBG)
		screen.SetContent(col, y, powerlineSep, nil, sepStyle)
		col++
	}

	return col
}
