package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResolveInlineDynamicBackendPersistsConcreteName(t *testing.T) {
	cfg := config.Config{
		Defaults: config.Defaults{Backend: config.DynamicBackend},
		Backends: map[string]config.Backend{"codex": {Command: "codex"}},
		BackendRouting: config.BackendRoutingConfig{Tiers: []config.BackendTier{
			{Backend: "codex", Probe: config.ProbeNone},
		}},
	}
	task := &model.Task{Backend: config.DynamicBackend}
	backend, resolved, err := resolveInlineDynamicBackend(task, config.Project{}, cfg)
	testutil.NoError(t, err)
	testutil.True(t, resolved)
	testutil.Equal(t, backend, "codex")
	testutil.Equal(t, task.Backend, "codex")
}

func TestResolveInlineDynamicBackendRejectsUnresolvedSelector(t *testing.T) {
	cfg := config.Config{
		Defaults:       config.Defaults{Backend: config.DynamicBackend},
		Backends:       map[string]config.Backend{"codex": {Command: "codex"}},
		Projects:       map[string]config.Project{},
		BackendRouting: config.BackendRoutingConfig{},
	}
	task := &model.Task{Backend: config.DynamicBackend}
	_, resolved, err := resolveInlineDynamicBackend(task, config.Project{}, cfg)
	testutil.True(t, resolved)
	testutil.Error(t, err)
	testutil.Equal(t, task.Backend, config.DynamicBackend)
}
