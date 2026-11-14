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

// effectiveResidentAccount is the account the resident starts on: its persisted
// account kept while the resident.accounts list allows it (or a manual move
// chose it) and it is not depleted, otherwise the list's first usable account.
// Returned in stored form ("" = default).
func (m *residentManager) effectiveResidentAccount(task *model.Task, cfg config.Config, _ string) string {
	by := db.OverrideByAuto
	if ov, ok, err := m.db.ResidentAccountOverride(task.ID); err != nil {
		residentLog("[resident] account marker read failed, treating as automatic: %v", err)
	} else if ok {
		by = ov.By
	}
	cur := ""
	if task.SessionID != "" || task.Account != "" || by == db.OverrideByManual {
		cur = agent.NormalizeAccount(task.Account)
	}
	picked, notes := agent.ResidentStartAccount(cfg, cur, by == db.OverrideByManual, agent.AccountUsage(cfg), m.now())
	for _, n := range notes {
		residentLog("[resident] accounts list: %s", n)
	}
	if cur != "" && picked != cur {
		residentLog("[resident] start account %s -> %s per resident.accounts", cur, picked)
	}
	return storedResidentAccount(picked)
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

// maybeAutoSwitch moves the resident along its resident.accounts list: off a
// depleted account to the next usable one, back up to a recovered preferred
// account, or onto the list when its account is no longer allowed. It runs from
// tick with a live session. See the resident-agent spec for the conditions; the
// rate-limit menu's needs-input state deliberately does not block it.
func (m *residentManager) maybeAutoSwitch(task *model.Task, cfg config.Config, sess agent.SessionHandle) {
	if !cfg.Resident.AutoSwitchAccount || m.mover == nil {
		m.switchTicks, m.noneEligible = 0, false
		return
	}
	now := m.now()
	cur := agent.NormalizeAccount(task.Account)
	manual := false
	if ov, ok, err := m.db.ResidentAccountOverride(task.ID); err == nil && ok {
		manual = ov.By == db.OverrideByManual
	}
	usage := agent.AccountUsage(cfg)
	dec := agent.DecideResidentAccount(cfg, cur, manual, usage, now)
	if dec.Target == "" && !dec.NoneUsable {
		m.switchTicks, m.noneEligible = 0, false
		return
	}
	if !m.sessionIdle(task.ID, sess, now) || m.runner.HasPendingRestart(task.ID) || m.recycling(task.ID) || agent.AccountMoveInFlight(task.ID) {
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
	if dec.NoneUsable {
		if !m.noneEligible {
			m.noneEligible = true
			residentLog("[resident] auto switch from %s refused: every account in resident.accounts is depleted (%s)", cur, usageSummary(usage[cur], now))
			m.postBoard(task, "decision",
				fmt.Sprintf("%s is out of usage budget on every listed account", cfg.Resident.Name),
				fmt.Sprintf("Account %s is depleted (%s) and every other account in resident.accounts is depleted too. It will move automatically as soon as one recovers.", cur, usageSummary(usage[cur], now)))
		}
		return
	}
	_, res, err := agent.MoveResidentAccount(m.db, m.mover, agent.AccountMoveInput{TaskID: task.ID, Account: dec.Target, Force: true}, agent.ResidentMoveOpts{By: db.OverrideByAuto, Cfg: &cfg})
	if err != nil && !errors.Is(err, agent.ErrAccountMoveRestartUnknown) {
		residentLog("[resident] auto switch %s -> %s (%s) failed: %v", cur, dec.Target, dec.Reason, err)
		return
	}
	m.switchTicks, m.noneEligible = 0, false
	residentLog("[resident] auto switched account %s to %s reason=%s (%s) resumed=%t started=%t restartErr=%v", res.From, res.To, dec.Reason, usageSummary(usage[cur], now), res.Resumed, res.Started, err)
	var text, subject, body string
	switch dec.Reason {
	case agent.ResidentMoveDepleted:
		text = fmt.Sprintf("You were switched from account %s to %s because %s hit its usage limit. Your conversation carried over. Continue whatever you were doing.", res.From, res.To, res.From)
		subject = fmt.Sprintf("%s switched accounts: %s to %s", cfg.Resident.Name, res.From, res.To)
		body = fmt.Sprintf("%s hit its usage limit (%s), so %s was moved to %s at %s. The conversation carried over (resumed=%t).", res.From, usageSummary(usage[cur], now), cfg.Resident.Name, res.To, now.Format(time.RFC822), res.Resumed)
	case agent.ResidentMoveBackUp:
		text = fmt.Sprintf("You were moved from account %s back to %s, your preferred account, because it has usage available again. Your conversation carried over. Continue whatever you were doing.", res.From, res.To)
		subject = fmt.Sprintf("%s moved back to %s", cfg.Resident.Name, res.To)
		body = fmt.Sprintf("%s has usage available again, so %s was moved from %s back to it at %s. The conversation carried over (resumed=%t).", res.To, cfg.Resident.Name, res.From, now.Format(time.RFC822), res.Resumed)
	default:
		text = fmt.Sprintf("You were moved from account %s to %s because %s is no longer in your account list. Your conversation carried over. Continue whatever you were doing.", res.From, res.To, res.From)
		subject = fmt.Sprintf("%s moved accounts: %s to %s", cfg.Resident.Name, res.From, res.To)
		body = fmt.Sprintf("%s is no longer in resident.accounts, so %s was moved to %s at %s. The conversation carried over (resumed=%t).", res.From, cfg.Resident.Name, res.To, now.Format(time.RFC822), res.Resumed)
	}
	if serr := m.Send(text, fmt.Sprintf("account-switch-%d", now.UnixNano())); serr != nil {
		residentLog("[resident] account switch notice to resident failed: %v", serr)
	}
	m.postBoard(task, "fyi", subject, body)
}

// sessionIdle reports whether the resident is parked: raw output quiet, or (for
// a session that keeps repainting while it waits, e.g. Claude Code's "Continuing
// automatically ... Esc to cancel" usage-limit wait) its emulated screen stable
// with no working affordance. Force is passed to the move only after this
// check, so the move's own busy refusal (raw idle only) cannot block a switch
// off a limit-parked session.
func (m *residentManager) sessionIdle(taskID string, sess agent.SessionHandle, now time.Time) bool {
	if cs, ok := sess.(agent.ContentIdleSession); ok {
		return m.idleTracker.IsIdle(taskID, cs, now)
	}
	return sess.IsIdle()
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
