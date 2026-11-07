package agent

import (
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
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
	if dynamic {
		// The account selected for a tier must be able to run its backend. A
		// project/explicit pin wins; otherwise the tier's own account wins over
		// the configured project/global default. Filter before scoring so an
		// incompatible earlier tier cannot hide a compatible later one.
		routingCfg.BackendRouting.Tiers = make([]config.BackendTier, 0, len(cfg.BackendRouting.Tiers))
		for _, tier := range cfg.BackendRouting.Tiers {
			b, ok := cfg.Backends[tier.Backend]
			account := pin
			if account == "" {
				account = tier.Account
			}
			if account == "" {
				account = cfg.ResolveAccount("", project)
			}
			if ok && cfg.AccountSupports(account, b.Command) {
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
			Backend: dynamicFallbackBackend(cfg, pin, project), Account: pin,
			Reason: "dynamic tiers unavailable; concrete default fallback", Step: step,
		}
	}
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	return backendtier.Choice{Backend: backend, Account: pin, Reason: "explicit/project/default backend; no eligible tier", Step: step}
}

func resolveDynamicBackendName(cfg config.Config, account, project string) string {
	return resolveTaskChoiceLocked(config.DynamicBackend, project, cfg, account).Backend
}

func dynamicFallbackBackend(cfg config.Config, account, project string) string {
	backend := concreteDefaultBackend(cfg)
	if backend == "" {
		return backend
	}
	if account == "" {
		account = cfg.ResolveAccount("", project)
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

// Preview failure sentinels, mapped to 404/422 by the REST layer.
var (
	ErrPreviewProjectNotFound    = errors.New("project not found")
	ErrPreviewBackendUnavailable = errors.New("backend unavailable")
)

// RoutingPreview is the advisory, side-effect-free routing outcome.
type RoutingPreview struct {
	Backend      string
	Kind         string
	Models       []string
	ModelPolicy  string
	DefaultModel string
	Reason       string
}

// PreviewRouting reports what task creation would resolve for the inputs,
// through the same ResolveTaskRouting and ResolveModel code. It reserves no
// burst-ledger spawn, creates no worktree or task, and never returns account
// identities.
func PreviewRouting(cfg config.Config, project, backend, archetype, profile string) (RoutingPreview, error) {
	if _, ok := cfg.Projects[project]; !ok {
		return RoutingPreview{}, fmt.Errorf("%w: %q", ErrPreviewProjectNotFound, project)
	}
	routing, err := ResolveTaskRouting(CreateInput{Project: project, Backend: backend}, cfg, false)
	if err != nil {
		return RoutingPreview{}, fmt.Errorf("%w: %w", ErrPreviewBackendUnavailable, err)
	}
	b, ok := cfg.Backends[routing.Backend]
	if !ok {
		return RoutingPreview{}, fmt.Errorf("%w: backend %q not configured", ErrPreviewBackendUnavailable, routing.Backend)
	}
	task := &model.Task{Project: project, Backend: routing.Backend, Archetype: archetype, Profile: profile}
	def, _ := ResolveModel(task, b, cfg)
	models := BackendModels(b)
	if models == nil {
		models = []string{}
	}
	return RoutingPreview{
		Backend:      routing.Backend,
		Kind:         string(FamilyOf(b.Command).Kind),
		Models:       models,
		ModelPolicy:  ModelPolicy(b),
		DefaultModel: def,
		Reason:       routing.Reason,
	}, nil
}
