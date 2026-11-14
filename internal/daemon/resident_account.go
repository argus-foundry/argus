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

// effectiveResidentAccount is the account a (re)start runs on. A start that
// will resume an existing conversation (task.SessionID set) NEVER changes the
// account: the transcript lives under the current account, and switching it here
// would silently lose the conversation (and skip the debounce, cooldown, notices
// and marker that the tick path applies). Moves off a depleted or delisted account
// happen on the tick, through the move core that copies the transcript. Only a
// fresh start with no conversation yet picks per resident.accounts. Returned in
// stored form ("" = default).
func (m *residentManager) effectiveResidentAccount(task *model.Task, cfg config.Config, _ string) string {
	if task.SessionID != "" {
		return task.Account
	}
	cur := ""
	if task.Account != "" {
		cur = agent.NormalizeAccount(task.Account)
	}
	picked, notes := agent.ResidentStartAccount(cfg, cur, false, agent.AccountUsage(cfg), m.now())
	for _, n := range notes {
		residentLog("[resident] accounts list: %s", n)
	}
	return storedResidentAccount(picked)
}

// MoveAccount is the manual resident account move (RPC). It refuses a busy
// session unless force is set. A named account is a manual hold; picking "auto"
// asks Solo to follow headroom, so it holds nothing.
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
	by := db.OverrideByManual
	if strings.TrimSpace(account) == agent.AutoMoveTarget {
		by = db.OverrideByFollow
	}
	_, res, err := agent.MoveResidentAccount(m.db, m.mover, agent.AccountMoveInput{TaskID: task.ID, Account: account, Force: force}, agent.ResidentMoveOpts{By: by, Cfg: &cfg})
	m.mu.Lock()
	m.switchTicks = 0
	m.mu.Unlock()
	if err != nil && !errors.Is(err, agent.ErrAccountMoveRestartUnknown) {
		residentLog("[resident] manual account move to %q refused: %v", account, err)
		return res, err
	}
	residentLog("[resident] manual account move %s to %s by=%s resumed=%t started=%t", res.From, res.To, by, res.Resumed, res.Started)
	return res, err
}

// setSwitchTicks updates the tick-loop counter under the manager mutex (MoveAccount
// runs on the RPC goroutine).
func (m *residentManager) setSwitchTicks(n int) {
	m.mu.Lock()
	m.switchTicks = n
	m.mu.Unlock()
}

func (m *residentManager) bumpSwitchTicks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.switchTicks++
	return m.switchTicks
}

// noneUsableEpisode reads the persisted "board item posted" flag once, then
// serves it from memory.
func (m *residentManager) noneUsableEpisode(taskID string) bool {
	m.mu.Lock()
	loaded, v := m.noneLoaded, m.noneEligible
	m.mu.Unlock()
	if loaded {
		return v
	}
	posted := m.metaValues(taskID)[db.MetaKeyNoneUsablePosted] == "1"
	m.mu.Lock()
	m.noneLoaded, m.noneEligible = true, posted
	m.mu.Unlock()
	return posted
}

// setNoneUsableEpisode records the episode flag in memory and, when it changed,
// in task_meta.
func (m *residentManager) setNoneUsableEpisode(taskID string, posted bool) {
	was := m.noneUsableEpisode(taskID)
	m.mu.Lock()
	m.noneEligible = posted
	m.mu.Unlock()
	if was == posted {
		return
	}
	val := "0"
	if posted {
		val = "1"
	}
	if err := m.db.SetMeta(taskID, db.ResidentMetaNamespace, db.MetaKeyNoneUsablePosted, val); err != nil {
		residentLog("[resident] persisting none-usable episode flag failed: %v", err)
	}
}

// maybeAutoSwitch moves the resident along its resident.accounts list: off a
// depleted account to the next usable one (urgent), back up to a recovered
// preferred account, or onto the list when its account is no longer allowed.
// It runs from tick with a live session. See the resident-agent spec. The
// rate-limit menu's needs-input state does not block an URGENT move; every
// other move also needs a quiet session (no pending prompt, no unsent draft).
func (m *residentManager) maybeAutoSwitch(task *model.Task, cfg config.Config, sess agent.SessionHandle) {
	if !cfg.Resident.AutoSwitchAccount || m.mover == nil {
		m.setSwitchTicks(0)
		m.setNoneUsableEpisode(task.ID, false)
		return
	}
	now := m.now()
	cur := agent.NormalizeAccount(task.Account)
	// A manual hold applies only while the resident is still on the account the
	// human picked.
	manual := false
	if ov, ok, err := m.db.ResidentAccountOverride(task.ID); err == nil && ok {
		manual = ov.By == db.OverrideByManual && ov.Account == cur
	}
	usage := agent.AccountUsage(cfg)
	dec := agent.DecideResidentAccount(cfg, cur, manual, usage, now)
	if dec.Target == "" && !dec.NoneUsable {
		m.setSwitchTicks(0)
		m.setNoneUsableEpisode(task.ID, false)
		return
	}
	quiet := dec.Urgent || dec.NoneUsable || (!m.waiting(task.ID) && !m.hasDraft(sess))
	if !quiet || !m.sessionIdle(task.ID, sess, now) || m.runner.HasPendingRestart(task.ID) || m.recycling(task.ID) || agent.AccountMoveInFlight(task.ID) {
		m.setSwitchTicks(0)
		return
	}
	if m.bumpSwitchTicks() < residentSwitchConfirmTicks {
		return
	}
	if last := m.db.ResidentLastAutoSwitch(task.ID); !last.IsZero() && now.Sub(last) < residentSwitchCooldown {
		return
	}
	if dec.NoneUsable {
		if !m.noneUsableEpisode(task.ID) {
			m.setNoneUsableEpisode(task.ID, true)
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
	m.setSwitchTicks(0)
	m.setNoneUsableEpisode(task.ID, false)
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

// hasDraft reports an unsent composer draft on the resident's screen. Unknown
// (not Claude's composer, or no marker on screen) reads as no draft, so it can
// never block a move forever.
func (m *residentManager) hasDraft(sess agent.SessionHandle) bool {
	if m.draftFn != nil {
		return m.draftFn(sess)
	}
	cs, ok := sess.(agent.ContentIdleSession)
	if !ok {
		return false
	}
	cols, rows := cs.PTYSize()
	tail := agent.SubstantiveTail(cs.RecentOutputTail, 64*1024, agent.NeedsInputMaxExpandBytes)
	m.draftMu.Lock()
	defer m.draftMu.Unlock()
	draft, found := m.draftView.InputDraft(tail, cols, rows)
	return found && strings.TrimSpace(draft) != ""
}

// residentAccountUsable is the check BuildCmd applies at spawn: the stored
// account must exist and support the resident's backend, or the spawn is refused
// (never silently run on another login).
func residentAccountUsable(cfg config.Config, backend, stored string) bool {
	b, ok := cfg.Backends[backend]
	return ok && cfg.ValidateAccount(stored) == nil && cfg.AccountSupports(stored, b.Command)
}

// recoverInvalidAccount handles a resuming resident whose stored account was
// removed from config.toml or lost its Claude login directory: BuildCmd would
// refuse every spawn forever, and the tick only runs for a live session. It
// moves the resident onto the list's first usable account through the normal
// move core (transcript copy, marker with by=auto, cooldown stamp), then tells
// Solo and posts a board item. When the old account's storage can no longer be
// resolved the conversation cannot be copied; Solo starts fresh on the new
// account and is told so. Returns false when the start must be abandoned.
func (m *residentManager) recoverInvalidAccount(task *model.Task, cfg config.Config, backend string) bool {
	from := agent.NormalizeAccount(task.Account)
	target, notes := agent.ResidentStartAccount(cfg, "", false, agent.AccountUsage(cfg), m.now())
	for _, n := range notes {
		residentLog("[resident] accounts list: %s", n)
	}
	if !residentAccountUsable(cfg, backend, storedResidentAccount(target)) {
		m.postNoneUsableOnce(task, cfg, fmt.Sprintf("account %s is gone and no account in resident.accounts can run the resident", from))
		m.failStart(fmt.Sprintf("account %q is no longer valid and no listed account can run the resident", from))
		return false
	}
	residentLog("[resident] stored account %s is no longer valid; moving to %s", from, target)
	carried := false
	_, res, err := agent.MoveResidentAccount(m.db, m.mover, agent.AccountMoveInput{TaskID: task.ID, Account: target, Force: true}, agent.ResidentMoveOpts{By: db.OverrideByAuto, Cfg: &cfg})
	switch {
	case err == nil || errors.Is(err, agent.ErrAccountMoveRestartUnknown):
		carried = res.Resumed
	default:
		// The old account's storage is unresolvable, so the transcript cannot be
		// copied: persist the new account with no conversation and start fresh.
		residentLog("[resident] move off invalid account %s failed (%v); starting fresh on %s", from, err, target)
		ov := db.ResidentAccountOverride{Account: target, By: db.OverrideByAuto}
		if perr := m.db.SetResidentAccountMove(task.ID, storedResidentAccount(target), "", ov); perr != nil {
			m.failStart(fmt.Sprintf("persist account recovery: %v", perr))
			return false
		}
	}
	fresh, err := m.db.ResidentTask()
	if err != nil || fresh == nil {
		m.failStart(fmt.Sprintf("reload resident after account recovery: %v", err))
		return false
	}
	task.Account, task.SessionID = fresh.Account, fresh.SessionID
	text := fmt.Sprintf("You were moved from account %s to %s because %s is no longer a valid Claude account. ", from, target, from)
	body := fmt.Sprintf("Account %s is no longer valid (removed from config or no Claude login), so %s was moved to %s at %s. ", from, cfg.Resident.Name, target, m.now().Format(time.RFC822))
	if carried {
		text += "Your conversation carried over. Continue whatever you were doing."
		body += "The conversation carried over."
	} else {
		text += "Your previous conversation could not be carried over, so you are starting fresh: reread your memory folder for open loops and continue."
		body += "The old account's storage was unavailable, so the conversation could NOT be carried over; Solo restarted fresh."
	}
	residentLog("[resident] recovered from invalid account %s -> %s carried=%t", from, target, carried)
	if serr := m.Send(text, fmt.Sprintf("account-recover-%d", m.now().UnixNano())); serr != nil {
		residentLog("[resident] account recovery notice to resident failed: %v", serr)
	}
	m.postBoard(task, "fyi", fmt.Sprintf("%s moved accounts: %s to %s", cfg.Resident.Name, from, target), body)
	return true
}

// postNoneUsableOnce posts the "nothing can run the resident" decision item once
// per episode (persisted flag shared with the depleted case).
func (m *residentManager) postNoneUsableOnce(task *model.Task, cfg config.Config, why string) {
	if m.noneUsableEpisode(task.ID) {
		return
	}
	m.setNoneUsableEpisode(task.ID, true)
	m.postBoard(task, "decision", fmt.Sprintf("%s cannot start: no usable account", cfg.Resident.Name), why+". Fix resident.accounts or the account config; it will restart automatically.")
}
