package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/gdamore/tcell/v2"
)

func TestBoardApprovalRequiresReviewAndExplicitAction(t *testing.T) {
	p := NewHeraPage(nil)
	p.board.items = []*db.HeraBoardItem{{ID: 4, ResidentApproval: true, Body: "Tool: Bash exact input echo probe\n{\"body\":\"**bold**  two spaces\"}\n\u202e"}}
	var decisions []bool
	p.OnApprovalDecision = func(item *db.HeraBoardItem, approve bool) {
		testutil.Equal(t, item.ID, int64(4))
		decisions = append(decisions, approve)
	}
	p.OnBoardDismiss = func(int64, int64) { t.Fatal("approval used generic dismissal") }
	press := func(r rune) { p.handleBoardKey(tcell.NewEventKey(tcell.KeyRune, r, 0), nil) }
	press('r')
	testutil.Equal(t, len(decisions), 0)
	testutil.Equal(t, p.board.expandedID, int64(4))
	press('r')
	press('x')
	testutil.DeepEqual(t, decisions, []bool{true, false})
	sim := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, sim.Init())
	t.Cleanup(sim.Fini)
	sim.SetSize(100, 16)
	p.board.draw(sim, 0, 0, 100, 16, " Board ", true, keymap.DefaultKeymap())
	sim.Show()
	testutil.Contains(t, simScreenText(sim), "approve once")
	testutil.Contains(t, simScreenText(sim), "exact input echo probe")
	testutil.Contains(t, simScreenText(sim), "**bold**  two spaces")
	testutil.Contains(t, simScreenText(sim), `\u202e`)
}
