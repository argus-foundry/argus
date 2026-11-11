package backendtier

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

const (
	ScopeAccount = "account"
	ScopeGlobal  = "global"
)

// TierUsage is the reading tier resolution scores one tier with. Account is
// nil for the global Codex reading, which is not tied to any account.
type TierUsage struct {
	PercentUsed float64    `json:"percent_used"`
	ResetsAt    *time.Time `json:"resets_at"`
	MeasuredAt  *time.Time `json:"measured_at"`
	Scope       string     `json:"scope"`
	Account     *string    `json:"account"`
}

// TierRevision identifies a tier list's content and order.
func TierRevision(tiers []config.BackendTier) string {
	h := sha256.New()
	for _, t := range tiers {
		_, _ = fmt.Fprintf(h, "%q\x00%q\x00%q\x00%d\n", t.Backend, t.Account, t.Probe, t.ThresholdPct)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// ProjectTierUsage returns the reading ResolveChoice uses for tier, from the
// same account selection and provider scope, or nil when routing sees none.
// Claude readings are per account; the Codex reading is the single global one
// and only scores a tier on the default account.
func ProjectTierUsage(cfg config.Config, tier config.BackendTier, claude map[string]usagebudget.Reading, codex func() (Reading, bool), now time.Time) *TierUsage {
	selected := tier.Account
	if selected == "" {
		selected = cfg.ResolveAccount("", "")
	}
	switch tier.Probe {
	case config.ProbeClaudeUsage:
		key := selected
		if key == "" {
			key = config.DefaultAccountName
		}
		r, ok := claude[key]
		if !ok || (!r.ResetAt.IsZero() && !r.ResetAt.After(now)) {
			return nil
		}
		acct := key
		return &TierUsage{PercentUsed: r.Percentage, ResetsAt: timePtr(r.ResetAt), MeasuredAt: timePtr(r.LastProbedAt), Scope: ScopeAccount, Account: &acct}
	case config.ProbeCodexUsage:
		if selected != "" && selected != config.DefaultAccountName {
			return nil
		}
		r, ok := codex()
		if !ok {
			return nil
		}
		return &TierUsage{PercentUsed: r.Percentage, ResetsAt: timePtr(r.ResetAt), MeasuredAt: timePtr(r.LastProbedAt), Scope: ScopeGlobal}
	}
	return nil
}

// TierUsageFingerprint is a compact signature of the readings for tiers,
// stable across calls when nothing changed.
func TierUsageFingerprint(cfg config.Config, tiers []config.BackendTier, claude map[string]usagebudget.Reading, codex func() (Reading, bool), now time.Time) string {
	var b strings.Builder
	for i, t := range tiers {
		u := ProjectTierUsage(cfg, t, claude, codex, now)
		if u == nil {
			fmt.Fprintf(&b, "%d=nil;", i)
			continue
		}
		var reset int64
		if u.ResetsAt != nil {
			reset = u.ResetsAt.Unix()
		}
		fmt.Fprintf(&b, "%d=%g@%d;", i, u.PercentUsed, reset)
	}
	return b.String()
}
