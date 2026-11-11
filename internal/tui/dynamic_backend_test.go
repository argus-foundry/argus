package tui

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResolveInlineDynamicBackendPersistsConcreteAccountChoice(t *testing.T) {
	cfg := config.Config{
		Defaults:       config.Defaults{Backend: config.DynamicBackend},
		DefaultAccount: "cx",
		Backends: map[string]config.Backend{
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
		},
		Accounts: map[string]config.Account{
			"work": {ClaudeConfigDir: t.TempDir()},
			"cx":   {CodexHome: t.TempDir()},
		},
		BackendRouting: config.BackendRoutingConfig{Tiers: []config.BackendTier{
			{Backend: "claude", Account: "work", Probe: config.ProbeNone},
		}},
	}
	task := &model.Task{Backend: config.DynamicBackend, Project: "p"}
	routing, resolved, err := resolveInlineDynamicBackend(task, config.Project{}, cfg)
	testutil.NoError(t, err)
	testutil.True(t, resolved)
	testutil.Equal(t, routing.Backend, "claude")
	testutil.Equal(t, task.Backend, "claude")
	testutil.Equal(t, task.Account, "work")
	testutil.True(t, task.AccountRouted)
	testutil.Equal(t, routing.ReservedBackend, "claude")
	testutil.Equal(t, routing.ReservedAccount, "work")
	t.Cleanup(func() { agent.ReleaseTaskRouting(routing) })
}

func TestResolveInlineDynamicBackendRejectsUnresolvedSelector(t *testing.T) {
	cfg := config.Config{
		Defaults:       config.Defaults{Backend: config.DynamicBackend},
		Backends:       map[string]config.Backend{"codex": {Command: "codex"}},
		BackendRouting: config.BackendRoutingConfig{},
	}
	task := &model.Task{Backend: config.DynamicBackend}
	_, resolved, err := resolveInlineDynamicBackend(task, config.Project{}, cfg)
	testutil.True(t, resolved)
	testutil.Error(t, err)
	testutil.Equal(t, task.Backend, config.DynamicBackend)
}
