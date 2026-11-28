package hera

import (
	"fmt"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/gdamore/tcell/v2"
)

var boardTabKinds = [5]string{"decision", "fyi", "action", "todo", "all"}
var boardTabLabels = [5]string{"Decisions", "FYIs", "Actions", "Todos", "All"}

func (b *boardView) sourceItems() []*db.HeraBoardItem {
	if b.allItems != nil {
		return b.allItems
	}
	return b.items
}

func (b *boardView) counts() [5]int {
	var counts [5]int
	for _, item := range b.sourceItems() {
		if item.Resolved || (!b.showDeferred && item.DeferredAt != nil) {
			continue
		}
		for n, kind := range boardTabKinds[:4] {
			if item.Kind == kind {
				counts[n]++
				counts[4]++
				break
			}
		}
	}
	return counts
}

func (b *boardView) filter(selected int64) {
	b.items = nil
	b.cursor = 0
	expanded := false
	for _, item := range b.allItems {
		if item.Resolved || (!b.showDeferred && item.DeferredAt != nil) {
			continue
		}
		if b.tab != 4 && item.Kind != boardTabKinds[b.tab] {
			continue
		}
		if item.ID == selected {
			b.cursor = len(b.items)
		}
		if item.ID == b.expandedID {
			expanded = true
		}
		b.items = append(b.items, item)
	}
	if !expanded {
		b.expandedID = 0
	}
}

func (p *HeraPage) changeBoardTab(tab int) {
	tab = max(0, min(4, tab))
	if tab == p.board.tab {
		return
	}
	var selected int64
	if item := p.board.selected(); item != nil {
		selected = item.ID
	}
	if p.board.allItems == nil {
		p.board.allItems = p.board.items
	}
	p.board.tab = tab
	if p.boardTabs == nil {
		p.boardTabs = make(map[int64]int)
	}
	p.boardTabs[p.boardOrchID] = tab
	p.board.filter(selected)
	p.board.offset = 0
	p.board.followCursor = true
	uxlog.Log("[board] tab orch=%d kind=%s count=%d", p.boardOrchID, boardTabKinds[tab], len(p.board.items))
}

// Tab labels and separators occupy one cell per rune, even at width 1.
func (b *boardView) drawTabs(screen tcell.Screen, x, y, width int) {
	counts := b.counts()
	labels := boardTabLabels
	length := func() int {
		n := 3 * (len(labels) - 1)
		for i, label := range labels {
			n += len(label) + len(fmt.Sprintf(" %d", counts[i]))
			if i == b.tab || counts[i] > 0 {
				n += 2 // selected outer padding or count-box padding
			}
		}
		return n
	}
	if length() > width {
		labels = [5]string{"D", "F", "A", "T", "All"}
	}
	colors := [5]tcell.Color{theme.ColorBoardDecision, theme.ColorBoardFYI, theme.ColorBoardAction, theme.ColorBoardTodo, theme.ColorBoardDecision}
	type cell struct {
		r     rune
		style tcell.Style
	}
	var cells []cell
	var starts, ends [5]int
	appendText := func(text string, style tcell.Style) {
		for _, r := range text {
			cells = append(cells, cell{r, style})
		}
	}
	for i, label := range labels {
		if i > 0 {
			appendText(" · ", theme.StyleDimmed)
		}
		starts[i] = len(cells)
		badge := theme.StyleNormal.Foreground(theme.ColorBoardBadgeInk).Background(colors[i]).Bold(true)
		if i == b.tab {
			appendText(fmt.Sprintf(" %s %d ", label, counts[i]), badge)
		} else {
			appendText(label+" ", theme.StyleNormal)
			if counts[i] == 0 {
				appendText("0", theme.StyleDimmed)
			} else {
				appendText(fmt.Sprintf(" %d ", counts[i]), badge)
			}
		}
		ends[i] = len(cells)
	}
	// Keep whole neighboring tabs so the window has no orphan separators.
	first, last := b.tab, b.tab
	for first > 0 && ends[last]-starts[first-1] <= width {
		first--
	}
	for last < len(labels)-1 && ends[last+1]-starts[first] <= width {
		last++
	}
	start, end := starts[first], ends[last]
	// When even the abbreviated selection cannot fit, show its label first
	// rather than spending the only visible cell on the leading padding.
	if end-start > width {
		start++
	}
	for n := start; n < end && n-start < width; n++ {
		screen.SetContent(x+n-start, y, cells[n].r, nil, cells[n].style)
	}
}

func (b *boardView) deferredCount() int {
	n := 0
	for _, i := range b.sourceItems() {
		if !i.Resolved && i.DeferredAt != nil {
			n++
		}
	}
	return n
}
