package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/argus-foundry/argus/internal/db"
)

func checkedTaskWorktree(ctx context.Context, path, repo string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	roots := []string{filepath.Join(db.DataDir(), "worktrees"), filepath.Join(repo, ".claude", "worktrees"), filepath.Join(home, ".claude", "worktrees")}
	managed := false
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolved, canonical)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			managed = true
			break
		}
	}
	if !managed {
		return "", fmt.Errorf("worktree is outside managed roots")
	}
	out, _, err := RunTaskGit(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", fmt.Errorf("worktree registration check failed: %w", err)
	}
	for _, field := range strings.Split(out, "\x00") {
		if strings.HasPrefix(field, "worktree ") && SamePath(strings.TrimPrefix(field, "worktree "), canonical) && !SamePath(canonical, repo) {
			return canonical, nil
		}
	}
	return "", fmt.Errorf("worktree is not registered in expected repository")
}
