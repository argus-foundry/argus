package daemon

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

// Automatic account switch tuning (add-resident-account-switch). Vars so tests
// can shorten them.
var (
	// residentSwitchConfirmTicks is how many consecutive ticks (about 10s) the
	// depleted-and-idle condition must hold before the resident is moved.
	residentSwitchConfirmTicks = 2
	// residentSwitchCooldown is the minimum gap between automatic switches. It
	// is persisted (db.ResidentLastAutoSwitch) so a daemon bounce keeps it.
	residentSwitchCooldown = 10 * time.Minute
)

// residentMover is the slice of the session runner a resident account move
// needs (agent.SessionRunner satisfies it).
type residentMover interface {
	Get(taskID string) agent.SessionHandle
	KickRerender(task *model.Task, cfg config.Config, rows, cols uint16) error
	Recycle(task *model.Task, cfg config.Config, rows, cols uint16) error
	HasPendingRestart(taskID string) bool
}

// storedResidentAccount maps an account name to the stored form ("" = default).
func storedResidentAccount(name string) string {
	if name == config.DefaultAccountName {
		return ""
	}
	return name
}

// effectiveResidentAccount is the account the resident starts on: the runtime
// override left by a resident move, else resident.account. An override is
// dropped (logged) when resident.account no longer equals the value it was
// made against (the human edited the setting) or its account is no longer
// usable for the resident's backend.
func (m *residentManager) effectiveResidentAccount(task *model.Task, cfg config.Config, backend string) string {
	ov, ok, err := m.db.ResidentAccountOverride(task.ID)
	if err != nil {
		residentLog("[resident] account override read failed, using resident.account: %v", err)
		return cfg.Resident.Account
	}
	if !ok {
		return cfg.Resident.Account
	}
	drop := func(why string) string {
		residentLog("[resident] account override %q (by %s) dropped: %s", ov.Account, ov.By, why)
		if cerr := m.db.ClearResidentAccountOverride(task.ID); cerr != nil {
			residentLog("[resident] clearing account override failed: %v", cerr)
		}
		return cfg.Resident.Account
	}
	if ov.Base != cfg.Resident.Account {
		return drop(fmt.Sprintf("resident.account changed from %q to %q", ov.Base, cfg.Resident.Account))
	}
	if err := cfg.ValidateAccount(ov.Account); err != nil {
		return drop(err.Error())
	}
	if b, found := cfg.Backends[backend]; !found || !cfg.AccountSupports(ov.Account, b.Command) {
		return drop("account does not support the resident's backend")
	}
	return storedResidentAccount(ov.Account)
}

// MoveAccount is the manual resident account move (RPC). It refuses a busy
// session unless force is set.
func (m *residentManager) MoveAccount(account string, force bool) (agent.AccountMoveResult, error) {
	cfg := m.cfgFn()
	if !cfg.Resident.Enabled {
		return agent.AccountMoveResult{}, errors.New("the resident is not enabled")
	}
	if m.mover == nil {
		return agent.AccountMoveResult{}, errors.New("account moves are not available")
	}
	task, err := m.db.ResidentTask()
	if err != nil {
		return agent.AccountMoveResult{}, err
	}
	if task == nil {
		return agent.AccountMoveResult{}, errors.New("the resident has not started yet")
	}
	_, res, err := agent.MoveResidentAccount(m.db, m.mover, agent.AccountMoveInput{TaskID: task.ID, Account: account, Force: force}, agent.ResidentMoveOpts{By: db.OverrideByManual, Cfg: &cfg})
	m.switchTicks = 0
	if err != nil && !errors.Is(err, agent.ErrAccountMoveRestartUnknown) {
		residentLog("[resident] manual account move to %q refused: %v", account, err)
		return res, err
	}
	residentLog("[resident] manual account move %s to %s resumed=%t started=%t", res.From, res.To, res.Resumed, res.Started)
	return res, err
}

// maybeAutoSwitch moves the resident off a depleted account. It runs from tick
// with a live session. See the resident-agent spec for the conditions; the
// rate-limit menu's needs-input state deliberately does not block it.
func (m *residentManager) maybeAutoSwitch(task *model.Task, cfg config.Config, sess agent.SessionHandle) {
	if !cfg.Resident.AutoSwitchAccount || m.mover == nil {
		m.switchTicks, m.noneEligible = 0, false
		return
	}
	now := m.now()
	cur := agent.NormalizeAccount(task.Account)
	usage := agent.AccountUsage(cfg)
	if !agent.AccountDepleted(usage[cur], now) {
		m.switchTicks, m.noneEligible = 0, false
		return
	}
	if !sess.IsIdle() || m.runner.HasPendingRestart(task.ID) || m.recycling(task.ID) || agent.AccountMoveInFlight(task.ID) {
		m.switchTicks = 0
		return
	}
	m.switchTicks++
	if m.switchTicks < residentSwitchConfirmTicks {
		return
	}
	if last := m.db.ResidentLastAutoSwitch(task.ID); !last.IsZero() && now.Sub(last) < residentSwitchCooldown {
		return
	}
	_, res, err := agent.MoveResidentAccount(m.db, m.mover, agent.AccountMoveInput{TaskID: task.ID, Account: agent.AutoMoveTarget}, agent.ResidentMoveOpts{By: db.OverrideByAuto, Cfg: &cfg})
	if err != nil && !errors.Is(err, agent.ErrAccountMoveRestartUnknown) {
		if !m.noneEligible && errors.Is(err, agent.ErrAccount) {
			m.noneEligible = true
			residentLog("[resident] auto switch from %s refused: %v", cur, err)
			m.postBoard(task, "decision",
				fmt.Sprintf("%s is out of usage budget and no other account is available", cfg.Resident.Name),
				fmt.Sprintf("Account %s is depleted (%s) and no other Claude account can take over: %v. It will switch automatically as soon as one recovers.", cur, usageSummary(usage[cur], now), err))
		} else if !errors.Is(err, agent.ErrAccount) {
			residentLog("[resident] auto switch from %s failed: %v", cur, err)
		}
		return
	}
	m.switchTicks, m.noneEligible = 0, false
	residentLog("[resident] auto switched account %s to %s (%s) resumed=%t started=%t restartErr=%v", res.From, res.To, usageSummary(usage[cur], now), res.Resumed, res.Started, err)
	text := fmt.Sprintf("You were switched from account %s to %s because %s hit its usage limit. Your conversation carried over. Continue whatever you were doing.", res.From, res.To, res.From)
	if serr := m.Send(text, fmt.Sprintf("account-switch-%d", now.UnixNano())); serr != nil {
		residentLog("[resident] account switch notice to resident failed: %v", serr)
	}
	m.postBoard(task, "fyi",
		fmt.Sprintf("%s switched accounts: %s to %s", cfg.Resident.Name, res.From, res.To),
		fmt.Sprintf("%s hit its usage limit (%s), so %s was moved to %s at %s. The conversation carried over (resumed=%t).", res.From, usageSummary(usage[cur], now), cfg.Resident.Name, res.To, now.Format(time.RFC822), res.Resumed))
}

// postBoard puts an item on the resident coordinator's human board; failures
// are logged, never fatal (the switch itself already happened).
func (m *residentManager) postBoard(task *model.Task, kind, subject, body string) {
	orch, err := m.db.EnsureResidentOrchestrator(task.ID)
	if err != nil {
		residentLog("[resident] board post skipped: orchestrator: %v", err)
		return
	}
	roles, err := m.db.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
	if err != nil || len(roles) == 0 {
		residentLog("[resident] board post skipped: coordinator role (err=%v)", err)
		return
	}
	if _, err := m.db.PostHeraBoardItem(roles[0].ID, kind, subject, body); err != nil {
		residentLog("[resident] board post failed: %v", err)
	}
}

func usageSummary(r usagebudget.Reading, now time.Time) string {
	five, week := usagebudget.LiveWindows(r, now)
	var parts []string
	if five != nil {
		parts = append(parts, fmt.Sprintf("5-hour %.0f%%", five.Percentage))
	}
	if week != nil {
		parts = append(parts, fmt.Sprintf("weekly %.0f%%", week.Percentage))
	}
	if len(parts) == 0 {
		return "no live reading"
	}
	return strings.Join(parts, ", ")
}
