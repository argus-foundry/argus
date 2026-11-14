package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/gdamore/tcell/v2"
)

type residentMoveCall struct {
	account string
	force   bool
}

// residentAccountApp is the resident tab on a config with a Claude "work"
// account, parked on the resident's own row.
func residentAccountApp(t *testing.T) (*App, *db.DB, tcell.SimulationScreen, func()) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := testDBWithConfig(t, moveTestTOML)
	testutil.NoError(t, d.SetConfigValue("resident.enabled", "true"))
	testutil.NoError(t, d.SetConfigValue("resident.name", "Solo"))
	task, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(task.ID)
	testutil.NoError(t, err)
	a := New(d, agent.NewRunner(nil), false)
	sim, stop := wireApp(t, a)
	sim.SetSize(200, 40)
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.heraPage.Machine().ToRail()
	})
	redrawUI(t, a)
	return a, d, sim, stop
}

func TestSmoke_ResidentAccountPicker_MovesResidentThroughDaemon(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	a, _, sim, stop := residentAccountApp(t)
	defer stop()
	calls := make(chan residentMoveCall, 1)
	a.residentMoveFn = func(account string, force bool) (daemon.ResidentMoveAccountResp, error) {
		calls <- residentMoveCall{account, force}
		return daemon.ResidentMoveAccountResp{OK: true, From: "default", To: account, Resumed: true, Started: true}, nil
	}

	sim.InjectKey(tcell.KeyRune, '>', 0)
	waitForCond(t, a.tapp, "picker open", func() bool { return a.mode == modeAccountPicker && a.accountPicker != nil })
	sim.InjectKey(tcell.KeyDown, 0, 0) // auto -> default (current)
	sim.InjectKey(tcell.KeyDown, 0, 0) // -> work
	sim.InjectKey(tcell.KeyEnter, 0, 0)

	select {
	case c := <-calls:
		testutil.Equal(t, c.account, "work")
	case <-time.After(5 * time.Second):
		t.Fatal("resident move RPC not called")
	}
	waitForCond(t, a.tapp, "picker closed", func() bool { return a.mode != modeAccountPicker && a.accountPicker == nil })
	waitForCond(t, a.tapp, "done notice", func() bool { return strings.Contains(a.header.Notice(), "default → work") })
}

func TestSmoke_ResidentAccountPicker_FailureShowsError(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	a, _, sim, stop := residentAccountApp(t)
	defer stop()
	called := make(chan struct{}, 1)
	a.residentMoveFn = func(string, bool) (daemon.ResidentMoveAccountResp, error) {
		called <- struct{}{}
		return daemon.ResidentMoveAccountResp{}, errors.New("account: no other Claude account is available")
	}
	sim.InjectKey(tcell.KeyRune, '>', 0)
	waitForCond(t, a.tapp, "picker open", func() bool { return a.accountPicker != nil })
	sim.InjectKey(tcell.KeyEnter, 0, 0) // auto
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("resident move RPC not called")
	}
	waitForCond(t, a.tapp, "error shown", func() bool {
		return strings.Contains(a.statusbar.Error(), "no other Claude account")
	})
}

func TestResidentAccountChoices_ClaudeOnlyNoHandoffAndAutoSkipsDepleted(t *testing.T) {
	now := time.Now()
	stubTUIAccountUsage(t, map[string]usagebudget.Reading{
		"default": {FiveHour: &usagebudget.Window{Percentage: 100, ResetAt: now.Add(time.Hour), LastProbedAt: now}},
		"work":    {FiveHour: &usagebudget.Window{Percentage: 10, ResetAt: now.Add(time.Hour), LastProbedAt: now}},
	})
	a, d, _, stop := residentAccountApp(t)
	defer stop()
	res, err := d.ResidentTask()
	testutil.NoError(t, err)
	choices, notice := a.accountMoveChoices(res)
	testutil.Equal(t, notice, "")
	var names []string
	for _, c := range choices {
		names = append(names, c.Name)
		testutil.False(t, strings.HasPrefix(c.Name, "@"))
	}
	testutil.Equal(t, strings.Join(names, ","), "auto,default,work")
	testutil.Contains(t, choices[0].Detail, "now work")
}

func TestSmoke_ResidentAccountPicker_InertOnWorkerRow(t *testing.T) {
	f := newResidentViewFixture(t)
	defer f.stop()
	f.selectWorker(t, 0)
	f.a.tapp.QueueEvent(rune1('>'))
	syncUI(t, f.a.tapp)
	readUI(t, f.a.tapp, func() {
		testutil.Equal(t, f.a.mode, modeTaskList)
		testutil.True(t, f.a.accountPicker == nil)
	})
}
