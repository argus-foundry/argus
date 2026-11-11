package tui

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
)

type residentBoardSession struct {
	*repaintSession
	writes  [][]byte
	origins []agentview.InputOrigin
}

func (s *residentBoardSession) WriteInput(b []byte, origin agentview.InputOrigin) (int, error) {
	s.writes = append(s.writes, append([]byte(nil), b...))
	s.origins = append(s.origins, origin)
	return len(b), nil
}

func residentBoardRole(t *testing.T, d *db.DB) int64 {
	t.Helper()
	orchs, err := d.ListResidentOrchestrators(false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(orchs), 1)
	roles, err := d.ListHeraRoles(orchs[0].ID, false)
	testutil.NoError(t, err)
	for _, role := range roles {
		if role.Kind == db.HeraKindCoordinator {
			return role.ID
		}
	}
	t.Fatal("missing resident coordinator")
	return 0
}

func TestSmoke_ResidentBoardToggleArrivalReplyDismiss(t *testing.T) {
	a, d := residentTestApp(t)
	role := residentBoardRole(t, d)
	sess := &residentBoardSession{repaintSession: newRepaintSession()}
	residentSession(t, a, d, sess)
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(200, 45)
	readUI(t, a.tapp, func() {
		a.heraPage.SetBoardAuto(true)
		a.switchTab(widget.TabResident)
		a.heraPage.Machine().ToRail()
	})
	item, err := d.PostHeraBoardItem(role, "fyi", "Tell Kuljit the skills repo moved", "Use thanx-ai/anutron-ai")
	testutil.NoError(t, err)
	readUI(t, a.tapp, func() { a.heraPage.Refresh() })
	redrawUI(t, a)
	assertVisible := func(needle string) {
		t.Helper()
		readUI(t, a.tapp, func() {
			w, h := sim.Size()
			testutil.Contains(t, screenText(sim, w, h), needle)
		})
	}
	assertVisible("Tell Kuljit the skills repo moved")
	assertVisible("Plan · Board (1)")
	sim.InjectKey(tcell.KeyRune, 'p', 0)
	syncUI(t, a.tapp)
	assertVisible("No plan authored")
	readUI(t, a.tapp, func() { a.heraPage.Refresh() })
	redrawUI(t, a)
	assertVisible("No plan authored")
	// Toggle from the Details region too, through the real event loop.
	readUI(t, a.tapp, func() { a.heraPage.Machine().SetRegion(hera.FocusAgent) })
	sim.InjectKey(tcell.KeyRune, 'p', 0)
	syncUI(t, a.tapp)
	assertVisible("Tell Kuljit the skills repo moved")
	sim.InjectKey(tcell.KeyRune, 'r', 0)
	syncUI(t, a.tapp)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.heraPage.Machine().State(), hera.FocusCoord)
		testutil.Equal(t, len(sess.writes), 1)
		testutil.Equal(t, string(sess.writes[0]), fmt.Sprintf("\x1b[200~Re: %s (#%d)\n\n\x1b[201~", item.Subject, item.ID))
		testutil.Equal(t, sess.origins[0], agentview.OriginUser)
		a.heraPage.Machine().SetRegion(hera.FocusAgent)
	})
	sim.InjectKey(tcell.KeyRune, 'x', 0)
	syncUI(t, a.tapp)
	waitForCond(t, a.tapp, "resident board human dismissal", func() bool {
		items, e := d.ListHeraBoardItems(role, true)
		return e == nil && len(items) == 1 && items[0].Resolved && items[0].ResolvedBy == "human"
	})
	assertVisible("No open board items")
}

func TestResidentBoardAttentionPoll(t *testing.T) {
	a, d := residentTestApp(t)
	role := residentBoardRole(t, d)
	item, err := d.PostHeraBoardItem(role, "fyi", "Reminder", "Do the thing")
	testutil.NoError(t, err)
	a.residentStateFn = func() (daemon.ResidentStateResp, error) { return daemon.ResidentStateResp{}, nil }
	_, stop := wireApp(t, a)
	defer stop()
	poll := func(want int) {
		t.Helper()
		readUI(t, a.tapp, func() {
			a.residentPolledAt = time.Time{}
			a.pollResidentState()
		})
		waitForCond(t, a.tapp, "resident board attention poll", func() bool {
			return !a.residentPollPending && a.residentSt.boardUnread == want
		})
	}
	poll(1)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ●")
		testutil.Equal(t, a.residentBox.DesiredHeight(), 1)
		a.switchTab(widget.TabResident)
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
		testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
		a.applyResidentState(daemon.ResidentStateResp{Unread: true})
		a.switchTab(widget.TabSettings)
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ●")
		a.applyResidentState(daemon.ResidentStateResp{Waiting: true})
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo ?")
	})
	items, err := d.ListHeraBoardItems(role, false)
	testutil.NoError(t, err)
	testutil.False(t, items[0].Read) // viewing the conversation never reads its board
	testutil.NoError(t, d.ReadHeraBoardItem(role, item.ID))
	poll(0)
	readUI(t, a.tapp, func() {
		testutil.Equal(t, a.header.TabLabel(widget.TabResident), "Solo")
		testutil.Equal(t, a.residentBox.DesiredHeight(), 0)
	})
}

func TestResidentBoardPalette(t *testing.T) {
	a, d := residentTestApp(t)
	role := residentBoardRole(t, d)
	_, err := d.PostHeraBoardItem(role, "fyi", "Reminder", "Details")
	testutil.NoError(t, err)
	a.heraPage.SetBoardAuto(true)
	a.switchTab(widget.TabResident)
	a.heraPage.Machine().ToRail()
	label := a.activeKeymap().ActionLabel
	rows := a.paletteApplicableActions()
	testutil.True(t, labelsOf(rows)[label(keymap.ActBoardToggle)])
	testutil.False(t, labelsOf(rows)[label(keymap.ActBoardReply)])
	a.heraPage.Machine().SetRegion(hera.FocusAgent)
	rows = a.paletteApplicableActions()
	for _, act := range keymap.ContextOrder(keymap.CtxHeraBoard) {
		testutil.True(t, labelsOf(rows)[label(act)])
	}
	for _, row := range rows {
		if row.Label == label(keymap.ActBoardToggle) {
			row.invoke()
		}
	}
	rows = a.paletteApplicableActions()
	testutil.True(t, labelsOf(rows)[label(keymap.ActBoardToggle)])
	testutil.False(t, labelsOf(rows)[label(keymap.ActBoardReply)])
	a.heraPage.Machine().SetRegion(hera.FocusCoord)
	rows = a.paletteApplicableActions()
	testutil.False(t, labelsOf(rows)[label(keymap.ActBoardToggle)]) // conversation typing belongs to PTY
}

func TestResidentBoardAttentionSurvivesStatePollError(t *testing.T) {
	a, d := residentTestApp(t)
	role := residentBoardRole(t, d)
	_, err := d.PostHeraBoardItem(role, "fyi", "Reminder", "Body")
	testutil.NoError(t, err)
	a.residentStateFn = func() (daemon.ResidentStateResp, error) {
		return daemon.ResidentStateResp{}, errors.New("state unavailable")
	}
	_, stop := wireApp(t, a)
	defer stop()
	readUI(t, a.tapp, func() { a.pollResidentState() })
	waitForCond(t, a.tapp, "board poll independent of reply state", func() bool {
		return !a.residentPollPending && a.header.TabLabel(widget.TabResident) == "Solo ●"
	})
}
