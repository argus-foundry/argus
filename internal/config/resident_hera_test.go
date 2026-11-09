package config

import (
	"slices"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentHeraPolicy(t *testing.T) {
	r := DefaultResidentConfig(100)
	testutil.Equal(t, r.MaxWorkers, 8)
	for _, name := range []string{"hera_spawn_worker", "hera_delete_worker", "hera_accept", "hera_inbox"} {
		t.Run(name, func(t *testing.T) { testutil.Equal(t, slices.Contains(r.AllowTools, "mcp__argus__"+name), true) })
	}
	testutil.Equal(t, slices.Contains(r.AskTools, "mcp__argus__hera_graduate_worker"), true)
	testutil.Equal(t, slices.Contains(r.DenyTools, "mcp__argus__hera_new_orchestrator"), true)
}
