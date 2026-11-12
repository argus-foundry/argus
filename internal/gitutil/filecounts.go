package gitutil

import (
	"strings"

	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	FileKindText        = "text"
	FileKindBinary      = "binary"
	FileKindDirectory   = "directory"
	FileKindUnavailable = "unavailable"

	maxUntrackedCountBytes = 8 << 20
	binarySniffBytes       = 8000
)

// FileChange is one changed path with line counts. Additions and Deletions
// are nil unless Kind is text; a failed count is never reported as zero.
type FileChange struct {
	Path         string `json:"path"`
	OriginalPath string `json:"original_path,omitempty"`
	Status       string `json:"status"`
	Additions    *int   `json:"additions"`
	Deletions    *int   `json:"deletions"`
	Kind         string `json:"kind"`
}

type numstatEntry struct {
	add, del int
	binary   bool
	orig     string
}

// collectFileChanges lists changed paths and counts their lines in a fixed
// number of git commands (no per-file subprocess). Per-file scope mirrors
// FetchFileDiff: uncommitted changes vs HEAD win, otherwise committed changes
// vs the merge-base, otherwise untracked content counted in-process.
func collectFileChanges(worktree, mergeBase string) []FileChange {
	statusOut, err := runGit(worktree, "status", "--porcelain=v1", "-z")
	if err != nil {
		uxlog.Log("[git-counts] status failed: %v", err)
		return nil
	}
	entries := parseStatusZ(statusOut)

	local, localErr := runNumstat(worktree, "HEAD")
	var branch map[string]numstatEntry
	var branchStatus []FileChange
	branchErr := error(nil)
	if mergeBase != "" {
		branch, branchErr = runNumstat(worktree, mergeBase+"..HEAD")
		if nsOut, err := runGit(worktree, "diff", "--name-status", "-z", mergeBase+"..HEAD"); err == nil {
			branchStatus = parseNameStatusZ(nsOut)
		} else {
			branchErr = err
		}
	}

	attrs, attrsErr := collectCountAttributes(worktree, append(append([]FileChange{}, entries...), branchStatus...))
	seen := make(map[string]bool, len(entries))
	files := make([]FileChange, 0, len(entries)+len(branchStatus))
	for _, e := range entries {
		if seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		files = append(files, resolveFileAttributes(worktree, e, local, localErr, branch, branchErr, attrs[e.Path], attrsErr))
	}
	for _, e := range branchStatus {
		if seen[e.Path] {
			continue
		}
		files = append(files, resolveFileAttributes(worktree, e, local, localErr, branch, branchErr, attrs[e.Path], attrsErr))
	}
	unavailable := 0
	for _, f := range files {
		if f.Kind == FileKindUnavailable {
			unavailable++
		}
	}
	uxlog.Log("[git-counts] collected files=%d skipped_unavailable=%d attributes_error=%v local_error=%v branch_error=%v", len(files), unavailable, attrsErr, localErr, branchErr)
	return files
}

func resolveFile(worktree string, e FileChange, local map[string]numstatEntry, localErr error, branch map[string]numstatEntry, branchErr error) FileChange {
	return resolveFileAttributes(worktree, e, local, localErr, branch, branchErr, fileCountAttributes{}, nil)
}

func resolveFileAttributes(worktree string, e FileChange, local map[string]numstatEntry, localErr error, branch map[string]numstatEntry, branchErr error, attrs fileCountAttributes, attrsErr error) FileChange {
	e.Kind, e.Additions, e.Deletions = FileKindUnavailable, nil, nil
	if strings.HasSuffix(e.Path, "/") {
		e.Kind = FileKindDirectory
		return e
	}
	if e.Status == "U" || attrsErr != nil {
		return e
	}
	if e.Status == "??" {
		return countUntrackedAttributes(worktree, e, attrs)
	}
	if attrs.textconv {
		return e
	}
	if ns, ok := local[e.Path]; ok {
		return applyNumstat(e, ns)
	}
	if localErr != nil {
		e.Kind = FileKindUnavailable
		return e
	}
	if ns, ok := branch[e.Path]; ok {
		return applyNumstat(e, ns)
	}
	if branchErr != nil {
		e.Kind = FileKindUnavailable
		return e
	}
	zero := 0
	e.Kind, e.Additions, e.Deletions = FileKindText, &zero, &zero
	return e
}

func applyNumstat(e FileChange, ns numstatEntry) FileChange {
	if e.OriginalPath == "" {
		e.OriginalPath = ns.orig
	}
	if ns.binary {
		e.Kind = FileKindBinary
		return e
	}
	a, d := ns.add, ns.del
	e.Kind, e.Additions, e.Deletions = FileKindText, &a, &d
	return e
}

func runNumstat(worktree, rng string) (map[string]numstatEntry, error) {
	out, err := runGit(worktree, "diff", "--numstat", "--no-renames", "-z", rng)
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}
