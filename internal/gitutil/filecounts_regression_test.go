package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func wantUnavailableCount(t *testing.T, f FileChange) {
	t.Helper()
	testutil.Equal(t, f.Kind, FileKindUnavailable)
	testutil.Nil(t, f.Additions)
	testutil.Nil(t, f.Deletions)
}

func TestFileCountsAncestorSwap(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	gitConfig(t, dir, "status.showUntrackedFiles", "all")
	writeFile(t, dir, "nested/new", "inside\n")
	outside := t.TempDir()
	writeFile(t, outside, "new", "outside\nsecret\n")
	calls := 0
	runGitHook = func() {
		calls++
		if calls == 2 {
			testutil.NoError(t, os.Rename(filepath.Join(dir, "nested"), filepath.Join(dir, "saved")))
			testutil.NoError(t, os.Symlink(outside, filepath.Join(dir, "nested")))
		}
	}
	t.Cleanup(func() { runGitHook = nil })
	wantUnavailableCount(t, byPath(collectFileChanges(dir, ""))["nested/new"])
}

func TestFileCountsConflicts(t *testing.T) {
	for _, mode := range []string{"modify", "add"} {
		t.Run(mode, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			if mode == "modify" {
				writeFile(t, dir, "f", "base\n")
				gitRun(t, dir, "add", "f")
				gitRun(t, dir, "commit", "-m", "base")
			}
			gitRun(t, dir, "checkout", "-b", "side")
			writeFile(t, dir, "f", "side\n")
			gitRun(t, dir, "add", "f")
			gitRun(t, dir, "commit", "-m", "side")
			gitRun(t, dir, "checkout", "master")
			writeFile(t, dir, "f", "main\n")
			gitRun(t, dir, "add", "f")
			gitRun(t, dir, "commit", "-m", "main")
			cmd := exec.Command("git", "merge", "side")
			cmd.Dir = dir
			testutil.Error(t, cmd.Run())
			f := byPath(collectFileChanges(dir, ""))["f"]
			testutil.Equal(t, f.Status, "U")
			wantUnavailableCount(t, f)
		})
	}
	for _, xy := range []string{"UU", "AA", "DD"} {
		t.Run(xy, func(t *testing.T) {
			f := parseStatusZ(xy + " f\x00")[0]
			wantUnavailableCount(t, resolveFile("", f, map[string]numstatEntry{"f": {add: 3}}, nil, nil, nil))
		})
	}
}

func TestFileCountsRenameMatchesDiff(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "staged", true: "committed"}[committed], func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			writeFile(t, dir, "old", "a\nb\nc\n")
			gitRun(t, dir, "add", "old")
			gitRun(t, dir, "commit", "-m", "seed")
			gitRun(t, dir, "branch", "base")
			gitRun(t, dir, "mv", "old", "new")
			if committed {
				gitRun(t, dir, "commit", "-m", "rename")
			}
			f := byPath(FetchGitStatus("t", dir, Options{BaseBranch: "base"}).Files)["new"]
			d := FetchFileDiff("t", dir, "new", Options{BaseBranch: "base"})
			testutil.NoError(t, d.Err)
			a, r := diffCounts(d.Diff)
			wantCounts(t, f, a, r)
			testutil.Equal(t, f.Status, "R")
			testutil.Equal(t, f.OriginalPath, "old")
		})
	}
}

func TestFileCountsTrackedTextconv(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	writeFile(t, dir, ".gitattributes", "*.dat diff=convert\n")
	gitConfig(t, dir, "diff.convert.textconv", "printf 'constant\\n'")
	writeFile(t, dir, "x.dat", "a\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "seed")
	writeFile(t, dir, "x.dat", "b\nc\n")
	d := FetchFileDiff("t", dir, "x.dat", Options{})
	testutil.NoError(t, d.Err)
	testutil.Equal(t, d.Diff, "")
	wantUnavailableCount(t, byPath(collectFileChanges(dir, ""))["x.dat"])
}

func TestFileCountsUntrackedAttributes(t *testing.T) {
	for _, attr := range []string{"-diff", "binary", "diff=convert", "filter=convert", "working-tree-encoding=UTF-16", "diff"} {
		t.Run(attr, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			writeFile(t, dir, ".gitattributes", "*.dat "+attr+"\n")
			gitConfig(t, dir, "diff.convert.textconv", "printf 'converted\\nextra\\n'")
			gitConfig(t, dir, "filter.convert.clean", "printf 'cleaned\\nextra\\n'")
			data := "raw\n"
			if attr == "diff" {
				data = "nul\x00\nline\n"
			}
			writeFile(t, dir, "new.dat", data)
			f := byPath(collectFileChanges(dir, ""))["new.dat"]
			switch attr {
			case "-diff", "binary":
				testutil.Equal(t, f.Kind, FileKindBinary)
				testutil.Nil(t, f.Additions)
				testutil.Nil(t, f.Deletions)
				d := FetchFileDiff("t", dir, "new.dat", Options{})
				testutil.NoError(t, d.Err)
				testutil.Contains(t, d.Diff, "Binary files")
			case "diff":
				d := FetchFileDiff("t", dir, "new.dat", Options{})
				testutil.NoError(t, d.Err)
				a, r := diffCounts(d.Diff)
				wantCounts(t, f, a, r)
			default:
				wantUnavailableCount(t, f)
			}
		})
	}
}

func TestFileCountsDeleteRecreate(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	writeFile(t, dir, "f", "old\n")
	gitRun(t, dir, "add", "f")
	gitRun(t, dir, "commit", "-m", "seed")
	gitRun(t, dir, "rm", "f")
	writeFile(t, dir, "f", "new\nmore\n")
	files := collectFileChanges(dir, "")
	testutil.Equal(t, len(files), 1)
	d := FetchFileDiff("t", dir, "f", Options{})
	testutil.NoError(t, d.Err)
	a, r := diffCounts(d.Diff)
	wantCounts(t, files[0], a, r)
}

func TestFileCountsLocalPriority(t *testing.T) {
	for _, stage := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstaged", true: "staged"}[stage], func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			writeFile(t, dir, "f", "a\nb\n")
			gitRun(t, dir, "add", "f")
			gitRun(t, dir, "commit", "-m", "seed")
			gitRun(t, dir, "branch", "base")
			writeFile(t, dir, "f", "a\nB\nc\n")
			gitRun(t, dir, "commit", "-am", "branch")
			writeFile(t, dir, "f", "a\nB\nc\nd\ne\n")
			if stage {
				gitRun(t, dir, "add", "f")
			}
			f := byPath(FetchGitStatus("t", dir, Options{BaseBranch: "base"}).Files)["f"]
			wantCounts(t, f, 2, 0)
			d := FetchFileDiff("t", dir, "f", Options{BaseBranch: "base"})
			testutil.NoError(t, d.Err)
			a, r := diffCounts(d.Diff)
			wantCounts(t, f, a, r)
		})
	}
}

func TestFileCountsSymlinksAndCap(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target")
	testutil.NoError(t, os.WriteFile(outside, []byte("many\nlines\n"), 0o600))
	for _, target := range []string{outside, "missing"} {
		name := filepath.Base(target)
		testutil.NoError(t, os.Symlink(target, filepath.Join(dir, name)))
		wantCounts(t, countUntracked(dir, FileChange{Path: name, Status: "??"}), 1, 0)
	}
	for _, size := range []int{maxUntrackedCountBytes, maxUntrackedCountBytes + 1} {
		t.Run(strings.Repeat("x", size-maxUntrackedCountBytes+1), func(t *testing.T) {
			writeFile(t, dir, "large", strings.Repeat("x", size))
			f := countUntracked(dir, FileChange{Path: "large", Status: "??"})
			if size > maxUntrackedCountBytes {
				wantUnavailableCount(t, f)
			} else {
				wantCounts(t, f, 1, 0)
			}
		})
	}
}
