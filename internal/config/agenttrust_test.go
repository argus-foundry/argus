package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAgentTrustConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.True(t, DefaultConfig().Agents.TrustWorktrees)
	path := filepath.Join(t.TempDir(), "config.toml")
	testutil.NoError(t, os.WriteFile(path, []byte("[agents]\ntrust_worktrees = false\n"), 0o600))
	loader := NewFileLoader(path)
	testutil.Equal(t, loader.Apply(DefaultConfig()).Agents.TrustWorktrees, false)
	testutil.NoError(t, loader.Err())
}
