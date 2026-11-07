package backendtier

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func TestProjectTierUsage_AgreesWithRouting(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	cfg := config.Config{
		Backends: map[string]config.Backend{"claude": {Command: "claude"}, "codex": {Command: "codex"}},
		Accounts: map[string]config.Account{"work": {ClaudeConfigDir: "/tmp/argus-tu-work", CodexHome: "/tmp/argus-tu-codex"}},
	}
	claude := map[string]usagebudget.Reading{
		"work":                    {Percentage: 95, ResetAt: reset},
		config.DefaultAccountName: {Percentage: 10, ResetAt: reset},
	}
	codex := func() (Reading, bool) { return Reading{Percentage: 20, ResetAt: reset}, true }

	for _, tc := range []struct {
		name  string
		tier  config.BackendTier
		known bool
		pct   float64
	}{
		{"claude named", config.BackendTier{Backend: "claude", Account: "work", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}, true, 95},
		{"claude blank is default", config.BackendTier{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}, true, 10},
		{"codex default", config.BackendTier{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 90}, true, 20},
		{"codex named unknown", config.BackendTier{Backend: "codex", Account: "work", Probe: config.ProbeCodexUsage, ThresholdPct: 90}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer SetUsageSource(func() (Usage, error) {
				u := Usage{Claude: claude}
				r := usagebudget.Reading{Percentage: 20, ResetAt: reset}
				u.Codex = &r
				return u, nil
			})()
			cfg.BackendRouting.Tiers = []config.BackendTier{tc.tier}
			got := ProjectTierUsage(cfg, tc.tier, claude, codex, now)
			testutil.Equal(t, got != nil, tc.known)
			if tc.known {
				testutil.Equal(t, got.PercentUsed, tc.pct)
			}
			choice := ResolveChoice(cfg, "", "")
			routed := choice.Backend != ""
			// A known reading at/over its threshold is skipped by routing; an
			// unknown one fails open, so routing and the projection never disagree.
			testutil.Equal(t, routed, !tc.known || tc.pct < float64(tc.tier.ThresholdPct))
		})
	}

	t.Run("none probe has no reading", func(t *testing.T) {
		testutil.True(t, ProjectTierUsage(cfg, config.BackendTier{Backend: "claude", Probe: config.ProbeNone}, claude, codex, now) == nil)
	})
	t.Run("codex scope is global with null account", func(t *testing.T) {
		u := ProjectTierUsage(cfg, config.BackendTier{Backend: "codex", Probe: config.ProbeCodexUsage}, claude, codex, now)
		testutil.Equal(t, u.Scope, ScopeGlobal)
		testutil.True(t, u.Account == nil)
	})
	t.Run("fingerprint changes with reading", func(t *testing.T) {
		tiers := []config.BackendTier{{Backend: "claude", Probe: config.ProbeClaudeUsage}}
		a := TierUsageFingerprint(cfg, tiers, claude, codex, now)
		claude2 := map[string]usagebudget.Reading{config.DefaultAccountName: {Percentage: 11, ResetAt: reset}}
		testutil.True(t, a != TierUsageFingerprint(cfg, tiers, claude2, codex, now))
		testutil.Equal(t, a, TierUsageFingerprint(cfg, tiers, claude, codex, now))
	})
}
