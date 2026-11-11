package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

func codexWorktreeTrustFlag(worktree string) (string, error) {
	if worktree == "" {
		return "", fmt.Errorf("trust codex: empty worktree")
	}
	physical, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", fmt.Errorf("trust codex: resolve worktree: %w", err)
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return "", err
	}
	// JSON strings are compatible TOML basic-string keys. Codex 0.167.0
	// splits dots inside quoted CLI keys (including .argus), so pass one
	// inline projects table rather than projects."<path>".trust_level.
	key, _ := json.Marshal(physical) // strings always marshal successfully
	return " -c " + shellQuote("projects={"+string(key)+"={trust_level=\"trusted\"}}"), nil
}
