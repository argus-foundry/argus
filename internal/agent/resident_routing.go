package agent

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ResidentWorkerAccounts lists supporting accounts, including disabled ones,
// so Settings and routing share precisely the same eligibility predicate.
func ResidentWorkerAccounts(cfg config.Config) []string {
	var out []string
	for _, account := range cfg.AccountNames() {
		for _, family := range Families() {
			if family.UnattendedWorker != nil && len(family.Binaries) > 0 && cfg.AccountSupports(account, family.Binaries[0]) {
				out = append(out, account)
				break
			}
		}
	}
	return out
}

var ignoredWorkerAccounts sync.Map

// ResolveResidentWorker filters before ranking; there is deliberately no
// unfiltered default-backend fallback. It never runs on resume/restart.
func ResolveResidentWorker(cfg config.Config, project, backend, account string) (backendtier.Choice, error) {
	eligible := ResidentWorkerAccounts(cfg)
	allowed := make(map[string]bool, len(eligible))
	for _, name := range eligible {
		allowed[name] = true
	}
	for name := range cfg.Resident.WorkerAccounts {
		if !allowed[name] {
			if _, seen := ignoredWorkerAccounts.LoadOrStore(name, true); !seen {
				uxlog.Log("[resident] ignoring unknown or ineligible worker account %q", name)
				slog.Warn("[resident] ignoring unknown or ineligible worker account", "account", name)
			}
		}
	}
	for name := range allowed {
		if enabled, set := cfg.Resident.WorkerAccounts[name]; set && !enabled {
			delete(allowed, name)
		}
	}
	if len(eligible) == 0 {
		return backendtier.Choice{}, fmt.Errorf("no account supports an eligible unattended worker family")
	}
	if len(allowed) == 0 {
		return backendtier.Choice{}, fmt.Errorf("no account is enabled for resident workers")
	}
	enabled := make([]string, 0, len(allowed))
	for name := range allowed {
		enabled = append(enabled, name)
	}
	sort.Strings(enabled)
	if account != "" && !allowed[account] {
		return backendtier.Choice{}, fmt.Errorf("account %q is disabled or ineligible for resident workers; enabled: %v", account, enabled)
	}
	if backend != "" {
		b, ok := cfg.Backends[backend]
		if !ok || FamilyOf(b.Command).UnattendedWorker == nil {
			return backendtier.Choice{}, fmt.Errorf("backend %q is ineligible for unattended resident workers; enabled accounts: %v", backend, enabled)
		}
	}
	filtered := cfg
	filtered.BackendRouting.Tiers = nil
	seen := map[[2]string]bool{}
	appendTier := func(tier config.BackendTier) {
		b, ok := cfg.Backends[tier.Backend]
		if !ok || FamilyOf(b.Command).UnattendedWorker == nil || (backend != "" && backend != tier.Backend) {
			return
		}
		for _, name := range eligible {
			if !allowed[name] || !cfg.AccountSupports(name, b.Command) || (account != "" && name != account) || (tier.Account != "" && tier.Account != name) {
				continue
			}
			candidate := tier
			candidate.Account = name
			pair := [2]string{candidate.Backend, name}
			if seen[pair] {
				continue
			}
			seen[pair] = true
			filtered.BackendRouting.Tiers = append(filtered.BackendRouting.Tiers, candidate)
		}
	}
	for _, tier := range cfg.BackendRouting.Tiers {
		appendTier(tier)
	}
	// Configured tiers rank their explicit pairs first. Fill every remaining
	// eligible pair too: a missing account tier is not a disabled account.
	names := make([]string, 0, len(cfg.Backends))
	for name := range cfg.Backends {
		names = append(names, name)
	}
	sort.Strings(names)
	preferred := cfg.Projects[project].Backend
	if preferred == "" {
		preferred = cfg.Defaults.Backend
	}
	if preferred != "" {
		names = append([]string{preferred}, names...)
	}
	for _, name := range names {
		fallback := config.BackendTier{Backend: name, Probe: FamilyOf(cfg.Backends[name].Command).UnattendedUsageProbe, ThresholdPct: 90}
		if fallback.Probe == "" {
			fallback.Probe = config.ProbeNone
		}
		for _, tier := range cfg.BackendRouting.Tiers {
			if tier.Backend == name {
				fallback = tier
				fallback.Account = ""
				break
			}
		}
		appendTier(fallback)
	}
	selectionBackend := backend
	if selectionBackend == "" {
		selectionBackend = cfg.Projects[project].Backend
	}
	choice := backendtier.ResolveChoice(filtered, selectionBackend, account)
	if choice.Backend == "" && backend == "" {
		choice = backendtier.ResolveChoice(filtered, "", account)
	}
	if choice.Backend == "" && len(filtered.BackendRouting.Tiers) > 0 {
		// Ordered tiers can all be over their thresholds; preserve the filtered
		// candidate set and use the least-used eligible pair rather than escape
		// to the unfiltered default backend/account.
		filtered.BackendRouting.Strategy = config.StrategyHeadroom
		choice = backendtier.ResolveChoice(filtered, backend, account)
	}
	if choice.Backend == "" {
		return choice, fmt.Errorf("no eligible resident-worker backend/account pair; enabled accounts: %v", enabled)
	}
	uxlog.Log("[resident] worker routed backend=%s account=%s %s", choice.Backend, choice.Account, choice.Reason)
	return choice, nil
}
