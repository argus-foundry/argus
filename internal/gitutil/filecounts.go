package gitutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	seen := make(map[string]bool, len(entries))
	files := make([]FileChange, 0, len(entries)+len(branchStatus))
	for _, e := range entries {
		seen[e.Path] = true
		files = append(files, resolveFile(worktree, e, local, localErr, branch, branchErr))
	}
	for _, e := range branchStatus {
		if seen[e.Path] {
			continue
		}
		files = append(files, resolveFile(worktree, e, local, localErr, branch, branchErr))
	}
	return files
}

func resolveFile(worktree string, e FileChange, local map[string]numstatEntry, localErr error, branch map[string]numstatEntry, branchErr error) FileChange {
	if strings.HasSuffix(e.Path, "/") {
		e.Kind = FileKindDirectory
		return e
	}
	if e.Status == "??" {
		return countUntracked(worktree, e)
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
	if branchErr != nil || strings.Contains(e.Status, "U") {
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

func countUntracked(worktree string, e FileChange) FileChange {
	e.Kind = FileKindUnavailable
	full := filepath.Join(worktree, filepath.FromSlash(e.Path))
	info, err := os.Lstat(full)
	if err != nil {
		return e
	}
	if info.Mode()&os.ModeSymlink != 0 {
		one, zero := 1, 0
		e.Kind, e.Additions, e.Deletions = FileKindText, &one, &zero
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > maxUntrackedCountBytes {
		return e
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return e
	}
	sniff := data
	if len(sniff) > binarySniffBytes {
		sniff = sniff[:binarySniffBytes]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		e.Kind = FileKindBinary
		return e
	}
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	zero := 0
	e.Kind, e.Additions, e.Deletions = FileKindText, &lines, &zero
	return e
}

func runNumstat(worktree, rng string) (map[string]numstatEntry, error) {
	out, err := runGit(worktree, "diff", "--numstat", "-z", rng)
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

// parseNumstatZ parses `git diff --numstat -z`. Renames are emitted as
// "add\tdel\t\0old\0new\0"; the result is keyed by the new path.
func parseNumstatZ(out string) map[string]numstatEntry {
	m := make(map[string]numstatEntry)
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\t", 3)
		if len(f) < 3 {
			continue
		}
		ns := numstatEntry{}
		if f[0] == "-" || f[1] == "-" {
			ns.binary = true
		} else {
			ns.add, _ = strconv.Atoi(f[0])
			ns.del, _ = strconv.Atoi(f[1])
		}
		path := f[2]
		if path == "" {
			if i+2 >= len(parts) {
				break
			}
			ns.orig, path = parts[i+1], parts[i+2]
			i += 2
		}
		m[path] = ns
	}
	return m
}

// parseStatusZ parses `git status --porcelain=v1 -z` into legacy-style single
// status codes ("M", "A", "D", "R", "??").
func parseStatusZ(out string) []FileChange {
	var files []FileChange
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if len(rec) < 4 {
			continue
		}
		xy, path := rec[:2], rec[3:]
		fc := FileChange{Path: path, Status: collapseXY(xy)}
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			if i+1 < len(parts) {
				fc.OriginalPath = parts[i+1]
				i++
			}
		}
		files = append(files, fc)
	}
	return files
}

func collapseXY(xy string) string {
	if xy == "??" {
		return "??"
	}
	if strings.ContainsAny(xy, "U") || xy == "AA" || xy == "DD" {
		return "U"
	}
	if xy[1] != ' ' {
		return string(xy[1])
	}
	return string(xy[0])
}

// parseNameStatusZ parses `git diff --name-status -z`.
func parseNameStatusZ(out string) []FileChange {
	var files []FileChange
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		code := parts[i]
		if code == "" {
			continue
		}
		fc := FileChange{Status: code[:1]}
		if code[0] == 'R' || code[0] == 'C' {
			if i+2 >= len(parts) {
				break
			}
			fc.OriginalPath, fc.Path = parts[i+1], parts[i+2]
			i += 2
		} else {
			if i+1 >= len(parts) {
				break
			}
			fc.Path = parts[i+1]
			i++
		}
		files = append(files, fc)
	}
	return files
}
