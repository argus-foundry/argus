package daemon

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const usageEventInterval = 30 * time.Second

type usageSignature struct {
	fp         string
	measuredAt time.Time
}

// usageWatcher emits account.usage_changed when an account/provider's
// projected usage differs from the previous tick. The first observation of a
// key is a baseline, not a change.
type usageWatcher struct {
	last      map[string]usageSignature
	tiersSeen bool
	tiersSig  string
}

func usageFingerprint(u *backendtier.ProviderUsage) usageSignature {
	if u == nil {
		return usageSignature{}
	}
	var b strings.Builder
	for _, w := range u.Windows {
		fmt.Fprintf(&b, "%s=%g@%d;", w.Kind, w.PercentUsed, w.ResetsAt.Unix())
	}
	return usageSignature{fp: b.String(), measuredAt: u.MeasuredAt}
}

func (w *usageWatcher) tick(cfg config.Config, now time.Time) {
	if w.last == nil {
		w.last = map[string]usageSignature{}
	}
	claude := agent.AccountUsage(cfg)
	for _, name := range cfg.AccountNames() {
		if cfg.ValidateAccount(name) != nil {
			continue
		}
		cur := map[string]*backendtier.ProviderUsage{}
		if _, _, err := cfg.ClaudeConfigDir(name); err == nil {
			r, ok := claude[name]
			cur["claude"] = backendtier.ClaudeProviderUsage(r, ok, now)
		}
		if home, _, err := cfg.CodexHome(name); err == nil {
			cur["codex"] = backendtier.CodexHomeUsage(home, now).Usage
		}
		for kind, u := range cur {
			key := name + "\x00" + kind
			sig := usageFingerprint(u)
			prev, seen := w.last[key]
			w.last[key] = sig
			if !seen || prev.fp == sig.fp {
				continue
			}
			payload := map[string]string{"account": name, "kind": kind}
			if u != nil {
				payload["measured_at"] = u.MeasuredAt.Format(time.RFC3339)
			}
			events.Emit(model.EventTypeAccountUsageChanged, "", payload)
			uxlog.Log("[usage] account=%s kind=%s usage changed (available=%t)", name, kind, u != nil)
		}
	}
	w.tickTiers(cfg, claude, now)
}

func (w *usageWatcher) tickTiers(cfg config.Config, claude map[string]usagebudget.Reading, now time.Time) {
	tiers := cfg.BackendRouting.Tiers
	sig := backendtier.TierUsageFingerprint(cfg, tiers, claude, backendtier.CachedReading, now)
	seen, prev := w.tiersSeen, w.tiersSig
	w.tiersSeen, w.tiersSig = true, sig
	if !seen || prev == sig {
		return
	}
	rev := backendtier.TierRevision(tiers)
	events.Emit(model.EventTypeBackendTiersUsageChanged, "", map[string]string{"revision": rev})
	uxlog.Log("[usage] backend tier readings changed revision=%s tiers=%d", rev, len(tiers))
}

func (d *Daemon) runUsageEventWatcher() {
	if strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	w := &usageWatcher{}
	ticker := time.NewTicker(usageEventInterval)
	defer ticker.Stop()
	for {
		w.tick(d.db.Config(), time.Now())
		select {
		case <-d.done:
			return
		case <-ticker.C:
		}
	}
}
