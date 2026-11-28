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

func boardTabRow(sim tcell.SimulationScreen, x, y, width int) string {
	var row strings.Builder
	for dx := range width {
		r, _, _ := sim.Get(x+dx, y)
		if r == "" {
			r = " "
		}
		row.WriteString(r)
	}
	return row.String()
}

func TestBoardTabStripStylesAndBounds(t *testing.T) {
	colors := [5]tcell.Color{theme.ColorBoardDecision, theme.ColorBoardFYI, theme.ColorBoardAction, theme.ColorBoardTodo, theme.ColorBoardDecision}
	for _, background := range []tcell.Color{tcell.ColorBlack, tcell.ColorWhite} {
		t.Run(fmt.Sprint(background), func(t *testing.T) {
			for tab, label := range boardTabLabels {
				for _, count := range []int{0, 1, 12} {
					t.Run(fmt.Sprintf("%s/%d", label, count), func(t *testing.T) {
						sim := tcell.NewSimulationScreen("")
						testutil.NoError(t, sim.Init())
						defer sim.Fini()
						sim.SetSize(100, 3)
						sim.SetStyle(tcell.StyleDefault.Background(background))
						sim.Clear()
						b := newBoardView()
						for kind := range 4 {
							for range count {
								b.allItems = append(b.allItems, &db.HeraBoardItem{Kind: boardTabKinds[kind]})
							}
						}
						b.tab = tab
						b.drawTabs(sim, 3, 1, 90)
						row := boardTabRow(sim, 3, 1, 90)
						selectedCount := count
						if tab == 4 {
							selectedCount *= 4
						}
						pill := fmt.Sprintf(" %s %d ", label, selectedCount)
						pos := strings.Index(row, pill)
						testutil.True(t, pos >= 0)
						if pos < 0 {
							return
						}
						pos = len([]rune(row[:pos]))
						for dx := range len(pill) {
							_, style, _ := sim.Get(3+pos+dx, 1)
							fg, bg, _ := style.Decompose()
							testutil.Equal(t, fg, theme.ColorBoardBadgeInk)
							testutil.Equal(t, bg, colors[tab])
						}
						for other, otherLabel := range boardTabLabels {
							if other == tab {
								continue
							}
							otherCount := count
							if other == 4 {
								otherCount *= 4
							}
							text := fmt.Sprintf("%s  %d ", otherLabel, otherCount)
							if count == 0 {
								text = otherLabel + " 0"
							}
							start := strings.Index(row, text)
							testutil.True(t, start >= 0)
							if start < 0 {
								continue
							}
							start = len([]rune(row[:start]))
							// The label and its separating space stay outside the count fill.
							for dx := 0; dx <= len(otherLabel); dx++ {
								_, style, _ := sim.Get(3+start+dx, 1)
								testutil.Equal(t, style, theme.StyleNormal)
							}
							for dx := len(otherLabel) + 1; dx < len(text); dx++ {
								_, style, _ := sim.Get(3+start+dx, 1)
								if count == 0 {
									testutil.Equal(t, style, theme.StyleDimmed)
								} else {
									fg, bg, _ := style.Decompose()
									testutil.Equal(t, fg, theme.ColorBoardBadgeInk)
									testutil.Equal(t, bg, colors[other])
								}
							}
						}
						testutil.Equal(t, strings.Count(row, " · "), 4)
						if count == 0 {
							var tabs []string
							for n, name := range boardTabLabels {
								text := name + " 0"
								if n == tab {
									text = " " + text + " "
								}
								tabs = append(tabs, text)
							}
							testutil.Equal(t, strings.TrimRight(row, " "), strings.TrimRight(strings.Join(tabs, " · "), " "))
						}
					})
				}
			}
		})
	}
	for tab, label := range boardTabLabels {
		for width := 0; width <= 85; width++ {
			t.Run(fmt.Sprintf("bounds/%s/%d", label, width), func(t *testing.T) {
				sim := tcell.NewSimulationScreen("")
				testutil.NoError(t, sim.Init())
				defer sim.Fini()
				sim.SetSize(100, 3)
				b := newBoardView()
				b.allItems = kindItems()
				b.tab = tab
				b.drawTabs(sim, 3, 1, width)
				for x := range 100 {
					if x < 3 || x >= 3+width {
						r, _, _ := sim.Get(x, 1)
						testutil.True(t, r == " " || r == "")
					}
				}
				if width == 0 {
					return
				}
				row := boardTabRow(sim, 3, 1, width)
				testutil.True(t, !strings.HasPrefix(strings.TrimSpace(row), "·"))
				testutil.True(t, !strings.HasSuffix(strings.TrimSpace(row), "·"))
				selectedText := label[:1]
				if width >= 7 {
					visibleLabel := label
					// This fixture's full strip occupies 60 cells, including padding.
					if width < 60 && tab != 4 {
						visibleLabel = label[:1]
					}
					selectedText = fmt.Sprintf(" %s %d ", visibleLabel, [5]int{1, 1, 3, 1, 6}[tab])
				}
				pos := strings.Index(row, selectedText)
				testutil.True(t, pos >= 0)
				if pos < 0 {
					return
				}
				pos = len([]rune(row[:pos]))
				for dx := range len(selectedText) {
					_, style, _ := sim.Get(3+pos+dx, 1)
					fg, bg, _ := style.Decompose()
					testutil.Equal(t, fg, theme.ColorBoardBadgeInk)
					testutil.Equal(t, bg, colors[tab])
				}
			})
		}
	}
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
	t.Log("SimulationScreen: [text] below denotes the actual continuous filled cells; brackets are capture annotations.")
	t.Logf("Fill palette: Decisions/All %s; FYIs %s; Actions %s; Todos %s; ink %s.", theme.ColorBoardDecision, theme.ColorBoardFYI, theme.ColorBoardAction, theme.ColorBoardTodo, theme.ColorBoardBadgeInk)
	capture := func(width int) string {
		sim.Clear()
		b.drawTabs(sim, 0, 0, width)
		var out strings.Builder
		filled := false
		for x := range width {
			r, style, _ := sim.Get(x, 0)
			_, bg, _ := style.Decompose()
			nowFilled := bg != tcell.ColorDefault
			if nowFilled != filled {
				if nowFilled {
					out.WriteString("[")
				} else {
					out.WriteString("]")
				}
				filled = nowFilled
			}
			out.WriteString(r)
		}
		if filled {
			out.WriteString("]")
		}
		return strings.TrimRight(out.String(), " ")
	}
	for tab, label := range boardTabLabels {
		t.Run(label, func(t *testing.T) {
			b.tab = tab
			for _, width := range []int{90, 40, 24, 10, 4, 1} {
				t.Logf("Width %d, %s selected: %s", width, label, capture(width))
			}
		})
	}
	b.allItems = nil
	b.tab = 4
	t.Logf("Empty board: %s", capture(90))
	for range 4 {
		b.allItems = append(b.allItems, &db.HeraBoardItem{Kind: "fyi"})
	}
	b.tab = 1
	t.Logf("Live feedback example: %s", capture(90))
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
	p.OnBoardComplete = func(i *db.HeraBoardItem, outcome string) {
		completed = i.ID
		testutil.Equal(t, outcome, "done")
	}
	for _, r := range []rune{'c', 'x'} {
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

func TestBoardTabsAttentionJumpIgnoresUnreadFYI(t *testing.T) {
	p := NewHeraPage(memDB(t))
	o := coordOf(1, "tabs", 10, "coord")
	o.Roles[0].BoardUnread = 1 // unread FYI counts as attention but is not a jump target
	o.Roles[0].BoardItems = []*db.HeraBoardItem{{ID: 1, Kind: "decision", Read: true}, {ID: 2, Kind: "fyi"}}
	p.Rail().SetModel(heramodel.Model{Active: []heramodel.OrchView{o}})
	testutil.True(t, p.SelectOrch(1))
	p.updateBoard(&o)
	testutil.Equal(t, p.board.tab, 0)
	// The jump hotkey targets only unread decisions, so an unread FYI alone
	// neither jumps nor broadens the tab filter.
	testutil.False(t, p.JumpToNextNeedsInput())
	testutil.Equal(t, p.board.tab, 0)
	// Ordinary refresh with an active filter must not repeatedly auto-show
	// resident boards because another kind remains unread.
	p.changeBoardTab(0)
	p.residentMode = true
	p.showBoard = false
	p.updateBoard(&o)
	testutil.False(t, p.showBoard)
}
