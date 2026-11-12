package gitutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Machine codes returned by DiscardFile via *DiscardError.
const (
	DiscardInvalidPath      = "invalid_path"
	DiscardRename           = "rename_requires_explicit_paths"
	DiscardConflict         = "conflict_state"
	DiscardUntrackedNeedsOK = "untracked_requires_delete"
	DiscardNoChanges        = "no_changes"
	DiscardGitFailed        = "git_failed"
)

// DiscardError is a classified discard failure; the API maps Code to a status.
type DiscardError struct {
	Code string
	Msg  string
}

func (e *DiscardError) Error() string { return e.Msg }

func discardErr(code, format string, a ...any) *DiscardError {
	return &DiscardError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// ValidateWorktreePath checks that rel names a single non-directory path that
// stays inside worktree after resolving symlinks, and returns its absolute
// location. The path need not exist (a deleted tracked file is valid).
func ValidateWorktreePath(worktree, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", discardErr(DiscardInvalidPath, "path is empty or malformed")
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", discardErr(DiscardInvalidPath, "path must be relative")
	}
	clean := filepath.Clean(rel)
	if clean != filepath.ToSlash(rel) && clean != rel {
		return "", discardErr(DiscardInvalidPath, "path must be normalized")
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." || part == "." || strings.EqualFold(part, ".git") {
			return "", discardErr(DiscardInvalidPath, "path is not allowed")
		}
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", discardErr(DiscardInvalidPath, "worktree unavailable")
	}
	abs := filepath.Join(root, clean)

	existing := filepath.Dir(abs)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		next := filepath.Dir(existing)
		if next == existing {
			break
		}
		existing = next
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil || !within(root, resolved) {
		return "", discardErr(DiscardInvalidPath, "path escapes the worktree")
	}

	fi, err := os.Lstat(abs)
	switch {
	case err == nil && fi.IsDir():
		return "", discardErr(DiscardInvalidPath, "path is a directory")
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		if target, rerr := filepath.EvalSymlinks(abs); rerr == nil && !within(root, target) {
			return "", discardErr(DiscardInvalidPath, "symlink escapes the worktree")
		}
	}
	return abs, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

type porcelainEntry struct {
	xy   string
	path string
	orig string
}

func parsePorcelainZ(out string) []porcelainEntry {
	fields := strings.Split(out, "\x00")
	var entries []porcelainEntry
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		e := porcelainEntry{xy: f[:2], path: f[3:]}
		if e.xy[0] == 'R' || e.xy[0] == 'C' || e.xy[1] == 'R' || e.xy[1] == 'C' {
			if i+1 < len(fields) {
				e.orig = fields[i+1]
				i++
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func isUnmerged(xy string) bool {
	return xy == "DD" || xy == "AU" || xy == "UD" || xy == "UA" || xy == "DU" || xy == "AA" || xy == "UU"
}

func runGitLiteral(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Writer(&stderr)
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// DiscardFile restores one path to HEAD, undoing staged and unstaged edits.
// Untracked and index-only (never committed) files are removed only when
// deleteUntracked is set. Renames and unmerged paths are refused. Containment
// is re-validated here, immediately before any mutation.
func DiscardFile(worktree, rel string, deleteUntracked bool) error {
	_, err := ValidateWorktreePath(worktree, rel)
	if err != nil {
		return err
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return discardErr(DiscardGitFailed, "open worktree failed")
	}
	defer func() { _ = root.Close() }()

	out, err := runGitLiteral(worktree, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return discardErr(DiscardGitFailed, "git status failed")
	}
	var entry *porcelainEntry
	for _, e := range parsePorcelainZ(out) {
		if e.orig == clean || (e.orig != "" && e.path == clean) {
			return discardErr(DiscardRename, "path is part of a rename; discard both paths explicitly")
		}
		if e.path == clean {
			e := e
			entry = &e
		}
	}
	if entry == nil {
		return discardErr(DiscardNoChanges, "path has no local changes")
	}
	if isUnmerged(entry.xy) {
		return discardErr(DiscardConflict, "path has an unresolved merge conflict")
	}

	inHead := true
	if _, herr := runGitLiteral(worktree, "cat-file", "-e", "HEAD:"+clean); herr != nil {
		inHead = false
	}

	if entry.xy == "??" || !inHead {
		if !deleteUntracked {
			return discardErr(DiscardUntrackedNeedsOK, "file is not in HEAD; set delete_untracked to remove it")
		}
		if _, err := ValidateWorktreePath(worktree, rel); err != nil {
			return err
		}
		if entry.xy != "??" {
			if _, err := runGitLiteral(worktree, "rm", "-f", "--quiet", "--cached", "--", clean); err != nil {
				return discardErr(DiscardGitFailed, "git rm failed")
			}
		}
		if err := root.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return discardErr(DiscardGitFailed, "remove failed")
		}
		return nil
	}

	if _, err := ValidateWorktreePath(worktree, rel); err != nil {
		return err
	}
	if _, err := runGitLiteral(worktree, "checkout", "HEAD", "--", clean); err != nil {
		return discardErr(DiscardGitFailed, "git checkout failed")
	}
	return nil
}

// AbsoluteGitDir returns the worktree's real git directory (HEAD and index
// live here, which for a linked worktree is outside the worktree itself).
func AbsoluteGitDir(worktree string) (string, error) {
	out, err := runGit(worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
