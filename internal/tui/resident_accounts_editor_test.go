package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/gdamore/tcell/v2"
)

func TestResidentAccountEntries_ListFirstThenOthersThenAuto(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{"personal": {ClaudeConfigDir: "/p"}, "work": {ClaudeConfigDir: "/w"}, "cx": {CodexHome: "/cx"}}
	cfg.Resident.Accounts = []string{"work", "ghost", "auto"}
	now := time.Now()
	usage := map[string]usagebudget.Reading{"work": {FiveHour: &usagebudget.Window{Percentage: 40, ResetAt: now.Add(time.Hour), LastProbedAt: now}}}
	rows := residentAccountEntries(cfg, usage, now)
	var got []string
	for _, r := range rows {
		mark := "-"
		if r.Checked {
			mark = "x"
		}
		got = append(got, mark+r.Name)
	}
	testutil.Equal(t, strings.Join(got, ","), "xwork,-default,-personal,xauto")
	testutil.Contains(t, rows[0].Detail, "5h 40%")

	cfg.Resident.Accounts = nil
	rows = residentAccountEntries(cfg, nil, now)
	testutil.True(t, rows[0].Checked)
	testutil.Equal(t, rows[0].Name, "default")
}

func TestSmoke_ResidentAccountsEditor_ReorderAndSave(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	a, d, sim, stop := residentAccountApp(t)
	defer stop()
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabSettings)
		a.settings.setCategory(catResident)
	})
	readUI(t, a.tapp, func() {
		selectRowInCategory(t, a.settings, catResident, srResident, resKeyAccounts)
	})
	sim.InjectKey(tcell.KeyEnter, 0, 0)
	waitForCond(t, a.tapp, "editor open", func() bool { return a.mode == modeResidentAccounts && a.residentAccountsModal != nil })

	// default(checked), personal? (config has only "work"): rows = default, work, auto.
	sim.InjectKey(tcell.KeyDown, 0, 0)
	sim.InjectKey(tcell.KeyRune, ' ', 0) // check work
	sim.InjectKey(tcell.KeyRune, 'K', 0) // move work above default
	sim.InjectKey(tcell.KeyEnter, 0, 0)
	waitForCond(t, a.tapp, "editor closed", func() bool { return a.mode != modeResidentAccounts && a.residentAccountsModal == nil })
	testutil.DeepEqual(t, d.Config().Resident.Accounts, []string{"work", "default"})
}

func TestSmoke_ResidentAccountsEditor_EscCancels(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	a, d, sim, stop := residentAccountApp(t)
	defer stop()
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabSettings)
		a.settings.setCategory(catResident)
		selectRowInCategory(t, a.settings, catResident, srResident, resKeyAccounts)
	})
	sim.InjectKey(tcell.KeyEnter, 0, 0)
	waitForCond(t, a.tapp, "editor open", func() bool { return a.residentAccountsModal != nil })
	sim.InjectKey(tcell.KeyEscape, 0, 0)
	waitForCond(t, a.tapp, "editor closed", func() bool { return a.residentAccountsModal == nil })
	testutil.Equal(t, len(d.Config().Resident.Accounts), 0)
}
