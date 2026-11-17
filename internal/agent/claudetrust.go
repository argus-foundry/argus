package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/inject"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

func trustClaudeWorktree(task *model.Task, cfg config.Config) error {
	if !cfg.Agents.TrustWorktrees {
		return nil
	}
	path, err := claudeTrustConfigPath(task, cfg)
	if err != nil {
		return err
	}
	worktreeErr := inject.TrustClaudeWorktree(path, task.Worktree)
	return errors.Join(worktreeErr, trustClaudeSkillsDir(path))
}

// claudeTrustConfigPath is the .claude.json the task's Claude launch reads.
// Account selection and backend env mappings must match launch. An explicit
// CLAUDE_CONFIG_DIR (even ~/.claude) stores .claude.json inside that dir.
func claudeTrustConfigPath(task *model.Task, cfg config.Config) (string, error) {
	dir, explicit, err := cfg.ClaudeConfigDir(task.Account)
	if err != nil {
		return "", err
	}
	backend, err := ResolveBackend(task, cfg)
	if err != nil {
		return "", err
	}
	if source := backend.EnvVars["CLAUDE_CONFIG_DIR"]; source != "" {
		dir, _ = resolveBackendEnvSource(cfg, source)
		explicit = dir != ""
	}
	if explicit {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// trustClaudeSkillsDir trusts the builtin skills workspace BuildCmd passes to
// Claude with --add-dir. Claude 2.1.326+ shows a trust dialog for an
// untrusted --add-dir directory on every launch. The workspace is
// materialized first because trust canonicalizes the path via EvalSymlinks.
func trustClaudeSkillsDir(path string) error {
	root, err := ensureSessionSkillsFn()
	if err != nil {
		return fmt.Errorf("skills dir: %w", err)
	}
	if root == "" {
		return nil
	}
	if err := inject.TrustClaudeWorktree(path, root); err != nil {
		return fmt.Errorf("skills dir: %w", err)
	}
	return nil
}

// trustResidentClaudeDirs seeds trust for the resident's working directory and
// --add-dir roots in the account it is launching on. The resident never goes
// through task creation and switches accounts on its own, so it is trusted at
// every launch instead. Fail-open: failures are logged and the launch goes on.
func trustResidentClaudeDirs(task *model.Task, cfg config.Config, dirs ...string) {
	if !cfg.Agents.TrustWorktrees {
		return
	}
	path, err := claudeTrustConfigPath(task, cfg)
	if err != nil {
		logResidentTrustFailure(task, "", err)
		return
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := inject.TrustClaudeWorktree(path, dir); err != nil {
			logResidentTrustFailure(task, dir, err)
		}
	}
}

func logResidentTrustFailure(task *model.Task, dir string, err error) {
	uxlog.Log("[trust] resident seed failed (continuing) dir=%q account=%q: %v", dir, task.Account, err)
	// BuildCmd runs in the daemon/supervisor, which never calls uxlog.Init.
	slog.Warn("[trust] resident seed failed (continuing)", "dir", dir, "account", task.Account, "err", err)
}
