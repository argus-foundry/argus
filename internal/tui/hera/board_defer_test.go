package hera

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestBoardDeferredVisibility(t *testing.T) {
	now := time.Now()
	for _, kind := range []string{"decision", "fyi", "action", "todo"} {
		t.Run(kind, func(t *testing.T) {
			p := NewHeraPage(nil)
			item := &db.HeraBoardItem{ID: 1, Kind: kind, Subject: "Parked", DeferredAt: &now}
			p.board.allItems = []*db.HeraBoardItem{item}
			p.board.filter(0)
			testutil.Equal(t, len(p.board.items), 0)
			testutil.Equal(t, p.board.counts()[4], 0)
			testutil.Contains(t, p.boardTitle(), "1 deferred")
			testutil.Equal(t, heramodel.BoardAttention(item), false)
			p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, 'D', 0), nil)
			testutil.Equal(t, len(p.board.items), 1)
			testutil.Equal(t, p.board.counts()[4], 1)
			testutil.Contains(t, boardKindLabel(item), "deferred")
			called := false
			p.OnBoardDefer = func(got *db.HeraBoardItem, deferred bool) {
				testutil.Equal(t, got.ID, item.ID)
				testutil.Equal(t, deferred, false)
				called = true
			}
			p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, 'd', 0), nil)
			testutil.Equal(t, called, true)
			p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, 'D', 0), nil)
			testutil.Equal(t, len(p.board.items), 0)
		})
	}
}
