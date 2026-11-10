package agent

import (
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// resolveTaskChoice shares backend precedence and carries the scored account
// into creation. Explicit/project accounts override tiers; global defaults do
// not prevent a tier from choosing an overflow identity. With reserve, the
// resolution also records the spawn in the burst ledger atomically
// (auto-account-routing); display/lookup callers pass false and stay pure.
//
// Unpinned order: (1) project account, (2) best account among the task's
// backend's tiers, (3) when the backend was not explicit and the project
// backend has no eligible tier, best tier overall on any backend. Coordinators
// always pass an explicit Claude backend, so they never reach step 3.
func resolveTaskChoice(explicit, project string, cfg config.Config, account string, reserve bool) backendtier.Choice {
	resolve := func() backendtier.Choice { return resolveTaskChoiceLocked(explicit, project, cfg, account) }
	if reserve {
		return backendtier.WithReservation(resolve)
	}
	return resolve()
}

func resolveTaskChoiceLocked(explicit, project string, cfg config.Config, account string) backendtier.Choice {
	backend := explicit
	if backend == "" {
		backend = cfg.Projects[project].Backend
	}
	dynamic := backend == config.DynamicBackend || (backend == "" && cfg.Defaults.Backend == config.DynamicBackend)
	if backend == config.DynamicBackend {
		backend = "" // Empty means consider every tier.
	}
	pin := account
	step := "pinned-account"
	if pin == "" {
		step = "backend-best"
		if backend == "" {
			// No task/project backend: ResolveChoice scans every tier, so this is
			// already the any-backend step (the global default does not count).
			step = "best-overall"
		}
		if p := cfg.ProjectAccounts[project]; p != "" && cfg.ValidateAccount(p) == nil {
			pin = p
			step = "project-account"
		}
	}
	routingCfg := cfg
	if dynamic && pin != "" {
		// A dynamic account pin must be able to run the candidate backend. Filter
		// incompatible tiers before scoring so an earlier Codex tier cannot hide
		// a later Claude tier compatible with a Claude-only account (and vice versa).
		routingCfg.BackendRouting.Tiers = make([]config.BackendTier, 0, len(cfg.BackendRouting.Tiers))
		for _, tier := range cfg.BackendRouting.Tiers {
			b, ok := cfg.Backends[tier.Backend]
			if ok && cfg.AccountSupports(pin, b.Command) {
				routingCfg.BackendRouting.Tiers = append(routingCfg.BackendRouting.Tiers, tier)
			}
		}
	}
	choice := backendtier.ResolveChoice(routingCfg, backend, pin)
	if choice.Backend == "" && pin == "" && explicit == "" && backend != "" && !dynamic {
		if overall := backendtier.ResolveChoice(cfg, "", ""); overall.Backend != "" {
			overall.Step = "best-overall"
			uxlog.Log("[routing] project backend %q has no eligible tier; best overall backend=%q account=%q", backend, overall.Backend, overall.Account)
			return overall
		}
	}
	if choice.Backend != "" {
		choice.Step = step
		return choice
	}
	if dynamic {
		return backendtier.Choice{
			Backend: dynamicFallbackBackend(cfg, pin), Account: pin,
			Reason: "dynamic tiers unavailable; concrete default fallback", Step: step,
		}
	}
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	return backendtier.Choice{Backend: backend, Account: pin, Reason: "explicit/project/default backend; no eligible tier", Step: step}
}

func resolveDynamicBackendName(cfg config.Config, account string) string {
	if backend := backendtier.ResolveBackend(cfg, account); backend != "" {
		return backend
	}
	return dynamicFallbackBackend(cfg, account)
}

func dynamicFallbackBackend(cfg config.Config, account string) string {
	backend := concreteDefaultBackend(cfg)
	if backend == "" || account == "" {
		return backend
	}
	b, ok := cfg.Backends[backend]
	if !ok || !cfg.AccountSupports(account, b.Command) {
		return ""
	}
	return backend
}

func concreteDefaultBackend(cfg config.Config) string {
	backend := cfg.Defaults.Backend
	if backend == "" || backend == config.DynamicBackend {
		return ""
	}
	if _, ok := cfg.Backends[backend]; !ok {
		return ""
	}
	return backend
}
