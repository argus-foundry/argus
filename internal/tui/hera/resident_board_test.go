package hera

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestResidentBoardLoadsAndHandlesKeys(t *testing.T) {
	f := newResidentFixture(t)
	roles, err := f.d.ListHeraRoles(f.orchID, false)
	testutil.NoError(t, err)
	var coord int64
	for _, r := range roles {
		if r.Kind == db.HeraKindCoordinator {
			coord = r.ID
		}
	}
	first, err := f.d.PostHeraBoardItem(coord, "fyi", "Tell Kuljit", "Skills repo moved")
	testutil.NoError(t, err)
	second, err := f.d.PostHeraBoardItem(coord, "fyi", "Second reminder", "Details")
	testutil.NoError(t, err)
	f.p.SetBoardAuto(true)
	f.enter(t)
	testutil.True(t, f.p.showBoard)
	testutil.Equal(t, f.p.boardTitle(), " Plan · Board (2) ")
	testutil.Equal(t, f.p.rail.Model().Active[0].CoordRole().BoardUnread, 2)
	testutil.Equal(t, f.p.board.selected().ID, first.ID)
	f.p.Machine().ToRail()
	f.p.InputHandler()(key('p'), noFocus)
	testutil.False(t, f.p.showBoard)
	f.p.Refresh()
	testutil.False(t, f.p.showBoard) // unchanged reminders must respect manual plan selection
	f.p.Machine().SetRegion(FocusAgent)
	f.p.InputHandler()(key('p'), noFocus)
	testutil.True(t, f.p.showBoard)
	var read, dismissed int64
	f.p.OnBoardRead = func(role, id int64, _ time.Time) {
		testutil.Equal(t, role, coord)
		read = id
	}
	f.p.OnBoardDismiss = func(role, id int64) {
		testutil.Equal(t, role, coord)
		dismissed = id
	}
	f.p.InputHandler()(key('j'), noFocus)
	testutil.Equal(t, f.p.board.selected().ID, second.ID)
	f.p.InputHandler()(key('k'), noFocus)
	testutil.Equal(t, f.p.board.selected().ID, first.ID)
	f.p.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), noFocus)
	testutil.Equal(t, read, first.ID)
	f.p.InputHandler()(key('x'), noFocus)
	testutil.Equal(t, dismissed, first.ID)
	// Rail filter input and worker terminals keep ownership of their keys.
	f.p.Machine().ToRail()
	f.p.InputHandler()(key('/'), noFocus)
	testutil.Equal(t, len(f.p.BoardActions()), 0)
	f.p.InputHandler()(key('p'), noFocus)
	testutil.Equal(t, f.p.rail.Filter(), "p")
	testutil.True(t, f.p.showBoard)
}

func TestResidentBoardAutoShowsArrival(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(map[bool]string{true: "auto", false: "manual"}[auto], func(t *testing.T) {
			f := newResidentFixture(t)
			f.p.SetBoardAuto(auto)
			f.enter(t)
			testutil.False(t, f.p.showBoard)
			coord := f.p.rail.Model().Active[0].CoordRole().RoleID
			_, err := f.d.PostHeraBoardItem(coord, "fyi", "Arrived while viewing", "Reminder")
			testutil.NoError(t, err)
			f.p.Refresh()
			testutil.Equal(t, f.p.showBoard, auto)
			testutil.Equal(t, len(f.p.board.items), 1)
			f.p.showBoard = false
			item := f.p.board.items[0]
			_, err = f.d.UpdateHeraBoardItem(coord, item.ID, "fyi", item.Subject, "Material update")
			testutil.NoError(t, err)
			f.p.Refresh()
			testutil.Equal(t, f.p.showBoard, auto)
		})
	}
}
