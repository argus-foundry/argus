package hera

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func kindItems() []*db.HeraBoardItem {
	return []*db.HeraBoardItem{
		{ID: 1, Kind: "decision", Subject: "Choose", RoleID: 9},
		{ID: 2, Kind: "todo", Subject: "Bump SW", RoleID: 9},
		{ID: 3, Kind: "action", Subject: "Run migration", RoleID: 9, Blocking: true, Command: "make  migrate **now**"},
		{ID: 4, Kind: "action", Subject: "Flip flag", RoleID: 9, Trigger: "after 1.42 ships", TriggerPending: true},
		{ID: 5, Kind: "fyi", Subject: "Heads up", RoleID: 9},
		{ID: 6, Kind: "action", Subject: "Ready step", RoleID: 9, Command: "echo hi"},
	}
}

func TestSortBoardItemsBands(t *testing.T) {
	items := kindItems()
	heramodel.SortBoardItems(items)
	var ids []int64
	for _, i := range items {
		ids = append(ids, i.ID)
	}
	// blocking first; ordinary by id; pending + todo last by id.
	testutil.DeepEqual(t, ids, []int64{3, 1, 5, 6, 2, 4})
	heramodel.SortBoardItems(items)
	testutil.Equal(t, items[0].ID, int64(3))
}

func TestBoardAttentionExcludesQuiet(t *testing.T) {
	for _, i := range kindItems() {
		t.Run(i.Subject, func(t *testing.T) {
			quiet := i.Kind == "todo" || i.TriggerPending
			testutil.Equal(t, heramodel.BoardAttention(i), !quiet)
		})
	}
	read := &db.HeraBoardItem{Kind: "action", Blocking: true, Read: true}
	testutil.False(t, heramodel.BoardAttention(read))
	testutil.False(t, heramodel.BoardAttention(&db.HeraBoardItem{Kind: "action", Blocking: true, Resolved: true}))
}

func TestBoardRenderKindsAndMetadata(t *testing.T) {
	items := kindItems()
	heramodel.SortBoardItems(items)
	b := &boardView{items: items, expandedID: 3}
	_, rows := drawBoardRows(t, b, 80, 24)
	text := strings.Join(rows, "\n")
	testutil.Contains(t, text, "[action BLOCKING] #3")
	testutil.Contains(t, text, "[action deferred] #4")
	testutil.Contains(t, text, "[todo] #2")
	testutil.Contains(t, text, "Step:")
	// Command is literal: repeated spaces and ** survive.
	testutil.Contains(t, text, "make  migrate **now**")
	b.expandedID = 4
	_, rows = drawBoardRows(t, b, 80, 24)
	testutil.Contains(t, strings.Join(rows, "\n"), "Trigger (deferred): after 1.42 ships")
}

func TestBoardClearCopyKeys(t *testing.T) {
	press := func(p *HeraPage, r rune) { p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, r, 0), nil) }
	type call struct {
		ID      int64
		Outcome string
	}
	for _, tc := range []struct {
		name    string
		item    *db.HeraBoardItem
		key     rune
		want    *call
		info    string
		dismiss bool
	}{
		{"clear action", &db.HeraBoardItem{ID: 3, Kind: "action"}, 'x', &call{3, "done"}, "", false},
		{"clear todo", &db.HeraBoardItem{ID: 2, Kind: "todo"}, 'x', &call{2, "done"}, "", false},
		{"clear deferred action", &db.HeraBoardItem{ID: 3, Kind: "action", TriggerPending: true}, 'x', &call{3, "done"}, "", false},
		{"clear decision", &db.HeraBoardItem{ID: 1, Kind: "decision"}, 'x', nil, "", true},
		{"clear fyi", &db.HeraBoardItem{ID: 5, Kind: "fyi"}, 'x', nil, "", true},
		{"clear resolved refused", &db.HeraBoardItem{ID: 1, Kind: "todo", Resolved: true}, 'x', nil, "open actions and todos", false},
		{"d on action inert", &db.HeraBoardItem{ID: 3, Kind: "action"}, 'd', nil, "", false},
		{"d on todo inert", &db.HeraBoardItem{ID: 2, Kind: "todo"}, 'd', nil, "", false},
		{"d on decision inert", &db.HeraBoardItem{ID: 1, Kind: "decision"}, 'd', nil, "", false},
		{"d on fyi inert", &db.HeraBoardItem{ID: 5, Kind: "fyi"}, 'd', nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewHeraPage(memDB(t))
			p.board.items = []*db.HeraBoardItem{tc.item}
			var got *call
			dismissed := false
			info := ""
			p.OnBoardComplete = func(i *db.HeraBoardItem, o string) {
				testutil.Equal(t, i, tc.item)
				got = &call{i.ID, o}
			}
			p.OnBoardDismiss = func(role, id int64) {
				testutil.Equal(t, role, tc.item.RoleID)
				testutil.Equal(t, id, tc.item.ID)
				dismissed = true
			}
			p.OnInfo = func(m string) { info = m }
			press(p, tc.key)
			testutil.DeepEqual(t, got, tc.want)
			testutil.Equal(t, dismissed, tc.dismiss)
			if tc.info != "" {
				testutil.Contains(t, info, tc.info)
			} else {
				testutil.Equal(t, info, "")
			}
		})
	}

	t.Run("x still dismisses fyi", func(t *testing.T) {
		p := NewHeraPage(memDB(t))
		p.board.items = []*db.HeraBoardItem{{ID: 5, Kind: "fyi"}}
		dismissed := false
		p.OnBoardDismiss = func(_, id int64) { dismissed = id == 5 }
		press(p, 'x')
		testutil.True(t, dismissed)
	})

	t.Run("copy sends exact command", func(t *testing.T) {
		p := NewHeraPage(memDB(t))
		cmd := "make  migrate **now**\ttab"
		p.board.items = []*db.HeraBoardItem{{ID: 3, Kind: "action", Command: cmd}}
		var got string
		p.OnBoardCopy = func(i *db.HeraBoardItem) { got = i.Command }
		press(p, 'c')
		testutil.Equal(t, got, cmd)
	})

	t.Run("copy without step", func(t *testing.T) {
		p := NewHeraPage(memDB(t))
		p.board.items = []*db.HeraBoardItem{{ID: 3, Kind: "action"}}
		called, info := false, ""
		p.OnBoardCopy = func(*db.HeraBoardItem) { called = true }
		p.OnInfo = func(m string) { info = m }
		press(p, 'c')
		testutil.False(t, called)
		testutil.Contains(t, info, "Nothing to copy")
	})
}

func TestBoardTitleCountsAttentionOnly(t *testing.T) {
	p := NewHeraPage(memDB(t))
	p.boardAuto = true
	p.board.items = kindItems() // all unread; todo + pending are quiet
	testutil.Contains(t, p.boardTitle(), "Board (4)")
}
