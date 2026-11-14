package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

// residentClaudeCommand is what AccountSupports is asked about: the resident
// only runs on a Claude-family backend.
const residentClaudeCommand = "claude"

// ResidentAccountEntries normalizes resident.accounts into the usable ordered
// list: account names ("default" included), optionally ending in
// config.AutoAccountEntry. Unset means ["default"]. Blank, duplicate, unknown
// and non-Claude entries are skipped, and entries after the first "auto" are
// dropped; notes describe each skip for the caller's log. A list that
// normalizes to nothing falls back to ["default"].
func ResidentAccountEntries(cfg config.Config) (entries, notes []string) {
	raw := cfg.Resident.Accounts
	if len(raw) == 0 {
		return []string{config.DefaultAccountName}, nil
	}
	seen := map[string]bool{}
	for i, r := range raw {
		r = strings.TrimSpace(r)
		switch r {
		case "":
			continue
		case config.AutoAccountEntry:
			entries = append(entries, config.AutoAccountEntry)
			if i < len(raw)-1 {
				notes = append(notes, "entries after auto are ignored")
			}
			goto done
		}
		name := NormalizeAccount(r)
		if seen[name] {
			notes = append(notes, fmt.Sprintf("duplicate account %q skipped", name))
			continue
		}
		if err := cfg.ValidateAccount(name); err != nil {
			notes = append(notes, fmt.Sprintf("account %q skipped: %v", name, err))
			continue
		}
		if !cfg.AccountSupports(name, residentClaudeCommand) {
			notes = append(notes, fmt.Sprintf("account %q skipped: no Claude login", name))
			continue
		}
		seen[name] = true
		entries = append(entries, name)
	}
done:
	if len(entries) == 0 {
		notes = append(notes, "no usable account in the list; using default")
		return []string{config.DefaultAccountName}, notes
	}
	return entries, notes
}

// residentRank is the preference rank of name in entries (lower is more
// preferred), or -1 when the list does not allow it. A trailing "auto" allows
// every Claude-capable account not listed explicitly, at the auto slot's rank.
func residentRank(cfg config.Config, entries []string, name string) int {
	for i, e := range entries {
		if e == name {
			return i
		}
	}
	if n := len(entries); n > 0 && entries[n-1] == config.AutoAccountEntry &&
		cfg.ValidateAccount(name) == nil && cfg.AccountSupports(name, residentClaudeCommand) {
		return n - 1
	}
	return -1
}

// residentUsable reports whether name can host the resident: it exists,
// supports Claude, and is not depleted at now.
func residentUsable(cfg config.Config, usage map[string]usagebudget.Reading, name string, now time.Time) bool {
	return cfg.ValidateAccount(name) == nil && cfg.AccountSupports(name, residentClaudeCommand) && !AccountDepleted(usage[name], now)
}

// AccountReadingFresh reports whether r has at least one live (valid, fresh)
// window, i.e. positive evidence about the account right now. An account whose
// reading went stale or was never read is NOT fresh: moving UP to a preferred
// account requires it, because a stale "no depleted window" is indistinguishable
// from an account that is still out of budget.
func AccountReadingFresh(r usagebudget.Reading, now time.Time) bool {
	five, week := usagebudget.LiveWindows(r, now)
	return five != nil || week != nil
}

// residentWanted is the account the list wants right now: the first non-depleted
// entry, with "auto" picking the lowest-usage non-depleted Claude account.
func residentWanted(cfg config.Config, entries []string, usage map[string]usagebudget.Reading, now time.Time) (string, bool) {
	for _, e := range entries {
		if e == config.AutoAccountEntry {
			picked, _, ok := pickMoveTargetWhere(cfg, usage, residentClaudeCommand, "", func(n string) bool { return !AccountDepleted(usage[n], now) })
			if ok {
				return picked, true
			}
			continue
		}
		if residentUsable(cfg, usage, e, now) {
			return e, true
		}
	}
	return "", false
}

// ResidentStartAccount picks the account a resident (re)start runs on. cur is
// the persisted account (normalized; "" when unknown). The persisted account is
// kept when the list allows it and it is not depleted; otherwise the list's
// first usable account wins; with every candidate depleted the persisted
// account (when it still exists) or the first listed entry is used so the
// resident still starts.
func ResidentStartAccount(cfg config.Config, cur string, usage map[string]usagebudget.Reading, now time.Time) (account string, notes []string) {
	entries, notes := ResidentAccountEntries(cfg)
	if cur != "" && residentUsable(cfg, usage, cur, now) && residentRank(cfg, entries, cur) >= 0 {
		return cur, notes
	}
	if want, ok := residentWanted(cfg, entries, usage, now); ok {
		return want, notes
	}
	if cur != "" && cfg.ValidateAccount(cur) == nil && cfg.AccountSupports(cur, residentClaudeCommand) {
		return cur, notes
	}
	first := entries[0]
	if first == config.AutoAccountEntry {
		first = config.DefaultAccountName
	}
	return first, notes
}

// ResidentMove reasons.
const (
	ResidentMoveDepleted = "depleted"
	// ResidentMoveBackUp is a restart-time move to a more preferred account that
	// has recovered; the running tick never makes it.
	ResidentMoveBackUp     = "move-back-up"
	ResidentMoveNotAllowed = "not-allowed"
)

// ResidentDecision is the automatic switch's verdict for a running resident.
type ResidentDecision struct {
	// Target and Reason are set when a move is due; both empty otherwise.
	Target, Reason string
	// Urgent is true only for a move off a depleted account: the session cannot
	// run turns there, so the move proceeds even past a pending prompt. Every
	// other move waits for a quiet session (no prompt, no unsent draft).
	Urgent bool
	// NoneUsable means the current account is depleted and no listed account
	// can take over.
	NoneUsable bool
}

// DecideResidentAccount decides whether the RUNNING resident (on normalized
// account cur; manual when its account was chosen by a manual move) should
// move: off a depleted account to the list's next usable one, or onto the list
// when its current account is no longer allowed (a manual pick is exempt from
// this one, so the human's escape hatch is not undone seconds later; the next
// restart re-picks per the list regardless). It never moves a healthy,
// allowed session "back up" to a more preferred account: that only happens at
// the next restart (DecideResidentRestartAccount), so Solo is never bounced
// between accounts while it has plenty of room.
func DecideResidentAccount(cfg config.Config, cur string, manual bool, usage map[string]usagebudget.Reading, now time.Time) ResidentDecision {
	entries, _ := ResidentAccountEntries(cfg)
	want, ok := residentWanted(cfg, entries, usage, now)
	if AccountDepleted(usage[cur], now) {
		if !ok {
			return ResidentDecision{NoneUsable: true}
		}
		return ResidentDecision{Target: want, Reason: ResidentMoveDepleted, Urgent: true}
	}
	if manual {
		return ResidentDecision{}
	}
	if residentRank(cfg, entries, cur) < 0 {
		if !ok || want == cur {
			return ResidentDecision{}
		}
		return ResidentDecision{Target: want, Reason: ResidentMoveNotAllowed}
	}
	return ResidentDecision{}
}

// DecideResidentRestartAccount re-picks the best account when the resident
// (re)starts: a cold start after an exit or crash, or a recycle. cur is the
// persisted normalized account (however it was chosen: a manual pick is a
// one-time move for that session, not a hold), and allowMoveUp is false while the automatic-switch cooldown is running (so a
// move's own restart can never bounce the resident again).
//
// A current account that is depleted, no longer valid for Claude or no longer in
// the list is replaced by the list's first usable account ("unknown reading =
// usable" when leaving). A healthy listed account moves UP to a more preferred
// listed one only when allowMoveUp and that account has a FRESH reading with
// headroom (a stale or unknown reading is not evidence of recovery). Target is
// empty when the resident stays put.
func DecideResidentRestartAccount(cfg config.Config, cur string, allowMoveUp bool, usage map[string]usagebudget.Reading, now time.Time) ResidentDecision {
	entries, _ := ResidentAccountEntries(cfg)
	rank := residentRank(cfg, entries, cur)
	if residentUsable(cfg, usage, cur, now) && rank >= 0 {
		if !allowMoveUp {
			return ResidentDecision{}
		}
		for i, e := range entries {
			if i >= rank {
				break
			}
			if e != config.AutoAccountEntry && residentUsable(cfg, usage, e, now) && AccountReadingFresh(usage[e], now) {
				return ResidentDecision{Target: e, Reason: ResidentMoveBackUp}
			}
		}
		return ResidentDecision{}
	}
	want, ok := residentWanted(cfg, entries, usage, now)
	if !ok || want == cur {
		return ResidentDecision{}
	}
	reason := ResidentMoveNotAllowed
	if AccountDepleted(usage[cur], now) {
		reason = ResidentMoveDepleted
	}
	return ResidentDecision{Target: want, Reason: reason, Urgent: reason == ResidentMoveDepleted}
}
