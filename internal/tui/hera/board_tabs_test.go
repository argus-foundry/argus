package hera

import (
	"fmt"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

func tabOrch(id int64, items []*db.HeraBoardItem) *heramodel.OrchView {
	return &heramodel.OrchView{ID: id, Roles: []heramodel.RoleView{{RoleID: id, Kind: db.HeraKindCoordinator, BoardItems: items}}}
}

func TestBoardTabsDefaultsAndMemory(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want int
	}{{"decision", 0}, {"fyi", 1}, {"action", 2}, {"todo", 3}, {"", 4}} {
		t.Run(tc.kind, func(t *testing.T) {
			p := NewHeraPage(memDB(t))
			var items []*db.HeraBoardItem
			if tc.kind != "" {
				items = []*db.HeraBoardItem{{ID: 1, Kind: tc.kind}}
			}
			o := tabOrch(1, items)
			p.updateBoard(o)
			testutil.Equal(t, p.board.tab, tc.want)
			p.changeBoardTab(4)
			p.updateBoard(tabOrch(2, kindItems()))
			testutil.Equal(t, p.board.tab, 0)
			p.updateBoard(nil)
			p.updateBoard(o)
			testutil.Equal(t, p.board.tab, 4)
		})
	}
}

func TestBoardTabsFilterAndCursor(t *testing.T) {
	p := NewHeraPage(memDB(t))
	o := tabOrch(1, kindItems())
	p.updateBoard(o)
	testutil.Equal(t, len(p.board.items), 1)
	p.changeBoardTab(4)
	testutil.Equal(t, p.board.selected().ID, int64(1))
	p.board.cursor = 3 // action #6
	p.board.expandedID = 6
	p.changeBoardTab(2)
	testutil.Equal(t, p.board.selected().ID, int64(6))
	testutil.Equal(t, p.board.expandedID, int64(6))
	p.changeBoardTab(1)
	testutil.Equal(t, p.board.selected().ID, int64(5))
	testutil.Equal(t, p.board.expandedID, int64(0))
	testutil.Equal(t, p.board.offset, 0)
	var dismissed int64
	p.OnBoardDismiss = func(_, id int64) { dismissed = id }
	p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, 'x', 0), nil)
	testutil.Equal(t, dismissed, int64(5))
	// Refresh never changes the selected kind or total counts.
	p.updateBoard(o)
	testutil.Equal(t, p.board.tab, 1)
	testutil.Equal(t, p.board.counts(), [5]int{1, 1, 3, 1, 6})
	p.changeBoardTab(0)
	p.handleBoardKey(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
	testutil.Equal(t, p.board.tab, 0)
	for range 7 {
		p.handleBoardKey(tcell.NewEventKey(tcell.KeyRight, 0, 0), nil)
	}
	testutil.Equal(t, p.board.tab, 4)
}

func TestBoardTabStripStylesAndBounds(t *testing.T) {
	for _, width := range []int{1, 4, 12, 32, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			sim := tcell.NewSimulationScreen("")
			testutil.NoError(t, sim.Init())
			defer sim.Fini()
			sim.SetSize(100, 4)
			b := newBoardView()
			b.allItems = kindItems()
			b.tab = 4
			b.drawTabs(sim, 3, 1, width)
			var row strings.Builder
			for x := 0; x < 100; x++ {
				r, _, _ := sim.Get(x, 1)
				if x < 3 || x >= 3+width {
					testutil.True(t, r == " " || r == "")
				} else {
					row.WriteString(r)
				}
			}
			if width == 80 {
				testutil.Contains(t, row.String(), "Decisions 1 ")
				testutil.Contains(t, row.String(), "All 6 ")
				_, style, _ := sim.Get(3+10, 1)
				_, bg, _ := style.Decompose()
				testutil.Equal(t, bg, theme.ColorBoardDecision)
			}
			if width >= 4 {
				testutil.Contains(t, row.String(), "All")
			}
		})
	}
	sim := tcell.NewSimulationScreen("")
	testutil.NoError(t, sim.Init())
	defer sim.Fini()
	sim.SetSize(90, 3)
	b := newBoardView()
	b.drawTabs(sim, 0, 0, 90)
	_, style, _ := sim.Get(10, 0)
	fg, bg, _ := style.Decompose()
	testutil.Equal(t, fg, theme.ColorDimmed)
	testutil.Equal(t, bg, tcell.ColorDefault)
}

func TestBoardTabCapture(t *testing.T) {
	sim := tcell.NewSimulationScreen("")
	testutil.NoError(t, sim.Init())
	defer sim.Fini()
	sim.SetSize(90, 3)
	b := newBoardView()
	for kind, n := range []int{4, 3, 2, 6} {
		for range n {
			b.allItems = append(b.allItems, &db.HeraBoardItem{Kind: boardTabKinds[kind]})
		}
	}
	b.tab = 0
	b.drawTabs(sim, 0, 0, 90)
	_, selectedStyle, _ := sim.Get(0, 0)
	testutil.Equal(t, selectedStyle, theme.StyleTitle)
	var row strings.Builder
	for x := 0; x < 90; x++ {
		r, _, _ := sim.Get(x, 0)
		row.WriteString(r)
	}
	t.Log(strings.TrimSpace(row.String()))
	t.Log("Selected Decisions label: accent cyan/bold. Badges: decision/All #93a5ff blue, FYI #67e8f9 teal, action #f0a868 orange, todo #9aa5b8 slate; ink #10141b. Zero: dim, no fill.")
	for n, label := range boardTabLabels {
		pos := runewidth.StringWidth(row.String()[:strings.Index(row.String(), label)]) + len(label)
		for dx := 0; dx < 3; dx++ {
			_, style, _ := sim.Get(pos+dx, 0)
			fg, bg, attr := style.Decompose()
			testutil.Equal(t, fg, theme.ColorBoardBadgeInk)
			testutil.Equal(t, bg, [5]tcell.Color{theme.ColorBoardDecision, theme.ColorBoardFYI, theme.ColorBoardAction, theme.ColorBoardTodo, theme.ColorBoardDecision}[n])
			testutil.True(t, attr&tcell.AttrBold != 0)
		}
	}
	testutil.Contains(t, row.String(), "Decisions 4  · FYIs 3  · Actions 2  · Todos 6  · All 15 ")
	for _, width := range []int{10, 24, 40} {
		sim.Clear()
		b.tab = 4
		b.drawTabs(sim, 0, 1, width)
		row.Reset()
		for x := 0; x < width; x++ {
			r, _, _ := sim.Get(x, 1)
			row.WriteString(r)
		}
		t.Logf("Width %d, All selected: %s", width, row.String())
	}
}

func TestBoardTabsRefreshAndFilteredActions(t *testing.T) {
	p := NewHeraPage(memDB(t))
	items := kindItems()
	items[0].Read = true
	items = append(items, &db.HeraBoardItem{ID: 99, Kind: "decision", Resolved: true})
	o := tabOrch(1, items)
	p.updateBoard(o)
	testutil.Equal(t, p.board.counts(), [5]int{1, 1, 3, 1, 6})
	p.changeBoardTab(2)
	var copied, completed int64
	p.OnBoardCopy = func(i *db.HeraBoardItem) { copied = i.ID }
	p.OnBoardComplete = func(i *db.HeraBoardItem, _ string) { completed = i.ID }
	for _, r := range []rune{'c', 'd'} {
		p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, r, 0), nil)
	}
	testutil.Equal(t, copied, int64(3))
	testutil.Equal(t, completed, int64(3))
	p.handleBoardKey(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	testutil.Equal(t, p.board.expandedID, int64(3))
	p.board.cursor = 1 // ordinary action #6
	o.Roles[0].BoardItems = nil
	for _, item := range items {
		if item.ID != 6 {
			o.Roles[0].BoardItems = append(o.Roles[0].BoardItems, item)
		}
	}
	p.updateBoard(o)
	testutil.Equal(t, p.board.selected().ID, int64(3))
	testutil.Equal(t, p.board.tab, 2)
	p.changeBoardTab(0)
	testutil.Contains(t, p.boardTitle(), "Board (2)") // other kinds still count attention
}

func TestBoardTabsAttentionJumpBroadensHiddenKind(t *testing.T) {
	p := NewHeraPage(memDB(t))
	o := coordOf(1, "tabs", 10, "coord")
	o.Roles[0].BoardUnread = 1
	o.Roles[0].BoardItems = []*db.HeraBoardItem{{ID: 1, Kind: "decision", Read: true}, {ID: 2, Kind: "fyi"}}
	p.Rail().SetModel(heramodel.Model{Active: []heramodel.OrchView{o}})
	testutil.True(t, p.SelectOrch(1))
	p.updateBoard(&o)
	testutil.Equal(t, p.board.tab, 0)
	testutil.True(t, p.JumpToNextNeedsInput())
	testutil.Equal(t, p.board.tab, 4)
	testutil.Equal(t, p.board.selected().ID, int64(2))
	// Ordinary refresh with an active filter must not repeatedly auto-show
	// resident boards because another kind remains unread.
	p.changeBoardTab(0)
	p.residentMode = true
	p.showBoard = false
	p.updateBoard(&o)
	testutil.False(t, p.showBoard)
}
