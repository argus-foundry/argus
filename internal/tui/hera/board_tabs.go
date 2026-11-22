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
		if item.Resolved {
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
			n += len(label) + len(fmt.Sprintf(" %d ", counts[i]))
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
	selectedStart, selectedEnd := 0, 0
	appendText := func(text string, style tcell.Style) {
		for _, r := range text {
			cells = append(cells, cell{r, style})
		}
	}
	for i, label := range labels {
		if i > 0 {
			appendText(" · ", theme.StyleDimmed)
		}
		style := theme.StyleNormal
		if i == b.tab {
			selectedStart = len(cells)
			style = theme.StyleTitle
		}
		appendText(label, style)
		badge := theme.StyleDimmed
		if counts[i] > 0 {
			badge = theme.StyleNormal.Foreground(theme.ColorBoardBadgeInk).Background(colors[i]).Bold(true)
		}
		appendText(fmt.Sprintf(" %d ", counts[i]), badge)
		if i == b.tab {
			selectedEnd = len(cells)
		}
	}
	start := 0
	if len(cells) > width && selectedEnd > width {
		start = min(selectedStart, selectedEnd-width)
	}
	for n := start; n < len(cells) && n-start < width; n++ {
		screen.SetContent(x+n-start, y, cells[n].r, nil, cells[n].style)
	}
}
