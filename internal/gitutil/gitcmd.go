package gitutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Options carries per-call comparison inputs shared by the git helpers. Add
// new fields here rather than new positional parameters.
type Options struct {
	// BaseBranch is the task's stored base ref (a stacked task's parent
	// branch). Empty means no requested base.
	BaseBranch string
}

// FetchGitStatus runs git commands in the given worktree directory.
// Intended to be called from a background goroutine.
func FetchGitStatus(taskID, worktree string, opts Options) GitStatusRefreshMsg {
	msg := GitStatusRefreshMsg{TaskID: taskID}

	if worktree == "" {
		return msg
	}

	out, err := runGit(worktree, "status", "--short")
	if err != nil {
		msg.Err = fmt.Errorf("git status: %w", err)
		return msg
	}
	msg.Status = strings.TrimRight(out, "\n")

	if out, err := runGit(worktree, "diff", "HEAD", "--stat"); err == nil {
		msg.Diff = strings.TrimRight(out, "\n")
	}

	msg.Comparison = resolveComparison(worktree, opts)
	if base := msg.Comparison.MergeBase; base != "" {
		if out, err := runGit(worktree, "diff", "--stat", base+"..HEAD"); err == nil {
			msg.BranchDiff = strings.TrimRight(out, "\n")
		} else {
			msg.Err = fmt.Errorf("git diff --stat: %w", err)
		}
		if out, err := runGit(worktree, "diff", "--name-status", base+"..HEAD"); err == nil {
			msg.BranchFiles = strings.TrimRight(out, "\n")
		} else {
			msg.Err = fmt.Errorf("git diff --name-status: %w", err)
		}
	}

	return msg
}

// findMergeBase finds the merge-base between HEAD and the upstream or default
// branch (no requested base).
func findMergeBase(worktree string) string {
	return resolveComparison(worktree, Options{}).MergeBase
}

// resolveComparison picks the comparison base: the requested base ref first,
// then the fallback chain in fallbackMergeBase. A requested base that cannot
// be resolved yields a non-nil FallbackReason alongside the fallback result.
func resolveComparison(worktree string, opts Options) Comparison {
	cmp := Comparison{RequestedBase: opts.BaseBranch}
	if opts.BaseBranch != "" {
		ref, sha, reason := mergeBaseWithRef(worktree, opts.BaseBranch)
		if reason == "" {
			cmp.ResolvedBase, cmp.MergeBase = ref, sha
			return cmp
		}
		cmp.FallbackReason = &reason
	}
	cmp.ResolvedBase, cmp.MergeBase = fallbackMergeBase(worktree)
	return cmp
}

// mergeBaseWithRef resolves a requested base, trying the local ref and then
// origin's remote-tracking ref. The ref is always passed after a revision
// check so option-looking values never reach git as flags.
func mergeBaseWithRef(worktree, base string) (ref, sha, reason string) {
	if strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t\n\x00") {
		return "", "", "requested base " + strconv.Quote(base) + " is not a valid ref"
	}
	for _, cand := range []string{base, "origin/" + base} {
		if _, err := runGit(worktree, "rev-parse", "--verify", "--quiet", "--end-of-options", cand+"^{commit}"); err != nil {
			continue
		}
		if out, err := runGit(worktree, "merge-base", "HEAD", cand); err == nil {
			if b := strings.TrimSpace(out); b != "" {
				return cand, b, ""
			}
		}
	}
	return "", "", "requested base " + strconv.Quote(base) + " not found or shares no history with HEAD"
}

// fallbackMergeBase finds the merge-base between HEAD and the upstream or
// default branch. Remote-tracking branches are tried before local master/main:
// all branch refs (local and remote-tracking alike) are shared across every
// worktree of a repo — only HEAD is per-worktree — so a local master is
// exposed to any arbitrary rewrite (rebase, reset, amend) that some other
// worktree or the primary checkout performs on it, silently jumping the
// merge-base to a stale ancestor. Remote-tracking refs are shared too, but
// they only ever change via an explicit fetch, which is a deliberate sync to
// the real upstream state (normally fast-forward) rather than an arbitrary
// history rewrite, so they're a safer fallback.
func fallbackMergeBase(worktree string) (ref, sha string) {
	if base, err := runGit(worktree, "merge-base", "HEAD", "HEAD@{upstream}"); err == nil {
		if b := strings.TrimSpace(base); b != "" {
			name := "@{upstream}"
			if n, err := runGit(worktree, "rev-parse", "--abbrev-ref", "HEAD@{upstream}"); err == nil && strings.TrimSpace(n) != "" {
				name = strings.TrimSpace(n)
			}
			return name, b
		}
	}
	for _, branch := range append(append([]string{}, priorityBranches...), "master", "main") {
		if base, err := runGit(worktree, "merge-base", "HEAD", branch); err == nil {
			if b := strings.TrimSpace(base); b != "" {
				return branch, b
			}
		}
	}
	return "", ""
}

// FetchFileDiff runs git diff for a specific file and returns raw output.
func FetchFileDiff(taskID, worktree, filePath string, opts Options) FileDiffMsg {
	msg := FileDiffMsg{TaskID: taskID, FilePath: filePath}
	if worktree == "" || filePath == "" {
		return msg
	}
	if _, err := runGit(worktree, "rev-parse", "--show-toplevel"); err != nil {
		msg.Err = fmt.Errorf("git worktree: %w", err)
		return msg
	}

	msg.Comparison = resolveComparison(worktree, opts)

	// Try uncommitted diff first (staged + unstaged) — raw output for parsing
	out, err := runGit(worktree, "--literal-pathspecs", "diff", "HEAD", "--", filePath)
	if err != nil {
		if _, headErr := runGit(worktree, "rev-parse", "--verify", "HEAD"); headErr != nil {
			out, err = runGit(worktree, "--literal-pathspecs", "diff", "--cached", "--", filePath)
			if err != nil {
				msg.Err = fmt.Errorf("git diff --cached: %w", err)
				return msg
			}
		}
	}
	if err == nil && out != "" {
		msg.Diff = out
		return msg
	}

	// Fall back to branch diff (committed changes vs merge-base)
	if base := msg.Comparison.MergeBase; base != "" {
		if out, err := runGit(worktree, "--literal-pathspecs", "diff", base+"..HEAD", "--", filePath); err == nil {
			msg.Diff = out
		} else {
			msg.Err = fmt.Errorf("git diff: %w", err)
			return msg
		}
	}

	// For untracked files, show the file contents as an "added" diff
	if msg.Diff == "" && !isTracked(worktree, filePath) {
		msg.Diff, msg.Err = untrackedFileDiff(worktree, filePath)
	}

	return msg
}

func isTracked(worktree, filePath string) bool {
	_, err := runGit(worktree, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", filePath)
	return err == nil
}

// FetchDirFiles lists untracked/changed files inside a directory in the worktree.
// Used for expanding directory entries in the file explorer.
func FetchDirFiles(taskID, worktree, dirPath string) DirFilesMsg {
	msg := DirFilesMsg{TaskID: taskID, DirPath: dirPath}
	if worktree == "" || dirPath == "" {
		return msg
	}

	// Path traversal guard: ensure resolved path stays within worktree
	fullDir := filepath.Join(worktree, dirPath)
	cleanWorktree := filepath.Clean(worktree) + string(filepath.Separator)
	if !strings.HasPrefix(filepath.Clean(fullDir)+string(filepath.Separator), cleanWorktree) {
		return msg
	}

	// Use git ls-files to list untracked + modified files (respects .gitignore)
	if out, err := runGit(worktree, "ls-files", "--others", "--modified", "--exclude-standard", "--", dirPath); err == nil && out != "" {
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			msg.Files = append(msg.Files, ChangedFile{
				Status: "A",
				Path:   line,
			})
		}
	}

	// Get actual status for each file to show correct indicators
	if len(msg.Files) > 0 {
		if statusOut, err := runGit(worktree, "status", "--short", "--", dirPath); err == nil && statusOut != "" {
			statusMap := make(map[string]string)
			for _, sline := range strings.Split(strings.TrimRight(statusOut, "\n"), "\n") {
				if len(sline) < 4 {
					continue
				}
				st := strings.TrimSpace(sline[:2])
				p := strings.TrimSpace(sline[3:])
				statusMap[p] = st
			}
			for i := range msg.Files {
				if st, ok := statusMap[msg.Files[i].Path]; ok {
					msg.Files[i].Status = st
				}
			}
		}
	}

	return msg
}

// ListRemoteBranches returns remote-tracking branch names (e.g. "origin/main")
// sorted with priority branches first. Returns nil if the path is not a git repo.
func ListRemoteBranches(repoDir string) []string {
	if repoDir == "" {
		return nil
	}
	out, err := runGit(repoDir, "for-each-ref", "--format=%(refname:short)", "refs/remotes/")
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "/HEAD") {
			continue
		}
		branches = append(branches, line)
	}
	return sortBranchesWithPriority(branches)
}

// priorityBranches defines the preferred ordering for branch selection.
// upstream first for fork workflows where upstream is the canonical remote.
// Dual use: also the remote-tracking fallback order tried by findMergeBase,
// so changing this list changes merge-base resolution, not just UI sort order.
var priorityBranches = []string{
	"upstream/master",
	"origin/master",
	"upstream/main",
	"origin/main",
}

// sortBranchesWithPriority returns branches with priority entries first (in
// order), followed by the remaining branches sorted alphabetically.
func sortBranchesWithPriority(branches []string) []string {
	if len(branches) == 0 {
		return nil
	}
	prioritySet := make(map[string]bool, len(priorityBranches))
	for _, b := range priorityBranches {
		prioritySet[b] = true
	}

	var result []string
	for _, pb := range priorityBranches {
		for _, b := range branches {
			if b == pb {
				result = append(result, b)
				break
			}
		}
	}

	var rest []string
	for _, b := range branches {
		if !prioritySet[b] {
			rest = append(rest, b)
		}
	}
	sort.Strings(rest)
	return append(result, rest...)
}

func runGit(dir string, args ...string) (string, error) {
	return runGitWithInput(dir, nil, nil, args...)
}

func runGitWithInput(dir string, input io.Reader, extraEnv []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdin = input
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.String(), err
}
