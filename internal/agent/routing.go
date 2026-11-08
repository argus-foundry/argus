package agent

import (
	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/uxlog"
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
	choice := backendtier.ResolveChoice(cfg, backend, pin)
	if choice.Backend == "" && pin == "" && explicit == "" && backend != "" {
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
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	return backendtier.Choice{Backend: backend, Account: pin, Reason: "explicit/project/default backend; no eligible tier", Step: step}
}
