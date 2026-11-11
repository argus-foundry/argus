package agent

import (
	"os"
	"path/filepath"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/inject"
	"github.com/argus-foundry/argus/internal/model"
)

func trustClaudeWorktree(task *model.Task, cfg config.Config) error {
	if !cfg.Claude.TrustWorktrees {
		return nil
	}
	// Account selection and backend env mappings must match launch. An explicit
	// CLAUDE_CONFIG_DIR (even ~/.claude) stores .claude.json inside that dir.
	dir, explicit, err := cfg.ClaudeConfigDir(task.Account)
	if err != nil {
		return err
	}
	backend, err := ResolveBackend(task, cfg)
	if err != nil {
		return err
	}
	if source := backend.EnvVars["CLAUDE_CONFIG_DIR"]; source != "" {
		dir, _ = resolveBackendEnvSource(cfg, source)
		explicit = dir != ""
	}
	var path string
	if explicit {
		path = filepath.Join(dir, ".claude.json")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".claude.json")
	}
	return inject.TrustClaudeWorktree(path, task.Worktree)
}
