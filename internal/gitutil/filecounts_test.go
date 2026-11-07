package gitutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	testutil.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func byPath(files []FileChange) map[string]FileChange {
	m := make(map[string]FileChange, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func diffCounts(diff string) (add, del int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return add, del
}

func wantCounts(t *testing.T, f FileChange, add, del int) {
	t.Helper()
	testutil.Equal(t, f.Kind, FileKindText)
	if f.Additions == nil || f.Deletions == nil {
		t.Fatalf("%s: nil counts", f.Path)
	}
	testutil.Equal(t, *f.Additions, add)
	testutil.Equal(t, *f.Deletions, del)
}

func TestFileChanges_ScopesMatchFileDiff(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	writeFile(t, dir, "committed.txt", "a\nb\nc\n")
	writeFile(t, dir, "staged.txt", "1\n2\n")
	writeFile(t, dir, "unstaged.txt", "x\ny\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base files")
	gitRun(t, dir, "branch", "feature-base")
	gitRun(t, dir, "checkout", "-q", "-b", "work")

	writeFile(t, dir, "committed.txt", "a\nB\nc\nd\n")
	gitRun(t, dir, "commit", "-am", "branch change")
	writeFile(t, dir, "staged.txt", "1\n")
	gitRun(t, dir, "add", "staged.txt")
	writeFile(t, dir, "unstaged.txt", "x\ny\nz\nw\n")
	writeFile(t, dir, "new.txt", "one\ntwo\nthree")

	msg := FetchGitStatus("t", dir, Options{BaseBranch: "feature-base"})
	testutil.NoError(t, msg.Err)
	files := byPath(msg.Files)
	testutil.Equal(t, len(files), 4)

	for _, p := range []string{"committed.txt", "staged.txt", "unstaged.txt", "new.txt"} {
		d := FetchFileDiff("t", dir, p, Options{BaseBranch: "feature-base"})
		a, r := diffCounts(d.Diff)
		wantCounts(t, files[p], a, r)
	}
	testutil.Equal(t, files["new.txt"].Status, "??")
	testutil.Equal(t, files["committed.txt"].Status, "M")
}

func TestFileChanges_SpecialPaths(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	writeFile(t, dir, "old name.txt", "l1\nl2\nl3\nl4\n")
	writeFile(t, dir, "bin.dat", "\x00\x01\x02")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "seed")

	gitRun(t, dir, "mv", "old name.txt", "new name.txt")
	writeFile(t, dir, "bin.dat", "\x00\x01\x02\x03")
	writeFile(t, dir, "ünï cødé 日本.txt", "u\n")
	writeFile(t, dir, "untracked-bin", "ab\x00cd")
	writeFile(t, dir, "empty.txt", "")
	writeFile(t, dir, "subdir/inner.txt", "i\n")

	msg := FetchGitStatus("t", dir, Options{})
	testutil.NoError(t, msg.Err)
	files := byPath(msg.Files)

	rn := files["new name.txt"]
	testutil.Equal(t, rn.OriginalPath, "old name.txt")
	testutil.Equal(t, rn.Status, "R")
	wantCounts(t, rn, 0, 0)

	b := files["bin.dat"]
	testutil.Equal(t, b.Kind, FileKindBinary)
	if b.Additions != nil || b.Deletions != nil {
		t.Fatal("binary counts must be nil")
	}
	testutil.Equal(t, files["untracked-bin"].Kind, FileKindBinary)
	wantCounts(t, files["ünï cødé 日本.txt"], 1, 0)
	wantCounts(t, files["empty.txt"], 0, 0)
	testutil.Equal(t, files["subdir/"].Kind, FileKindDirectory)
	if files["subdir/"].Additions != nil {
		t.Fatal("directory counts must be nil")
	}
}

func TestFileChanges_FailureIsNotZero(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "master")
	writeFile(t, dir, "x.txt", "x\n")
	gitRun(t, dir, "add", "x.txt")

	msg := FetchGitStatus("t", dir, Options{})
	files := byPath(msg.Files)
	testutil.Equal(t, files["x.txt"].Kind, FileKindUnavailable)
	if files["x.txt"].Additions != nil {
		t.Fatal("unavailable counts must be nil")
	}
	testutil.Contains(t, msg.Status, "x.txt")
}

func TestFileChanges_CleanAndEmptyWorktree(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	msg := FetchGitStatus("t", dir, Options{})
	if msg.Files == nil || len(msg.Files) != 0 {
		t.Fatalf("clean repo: want empty non-nil files, got %#v", msg.Files)
	}
	testutil.Equal(t, len(FetchGitStatus("t", "", Options{}).Files), 0)
}

func TestFileChanges_BatchedCommands(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	for i := 0; i < 30; i++ {
		writeFile(t, dir, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt", "x\n")
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "many")
	for i := 0; i < 30; i++ {
		writeFile(t, dir, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt", "y\nz\n")
	}
	writeFile(t, dir, "u1", "1\n")
	writeFile(t, dir, "u2", "1\n")

	gitCalls := 0
	runGitHook = func() { gitCalls++ }
	defer func() { runGitHook = nil }()
	msg := FetchGitStatus("t", dir, Options{})
	testutil.Equal(t, len(msg.Files), 32)
	if gitCalls > 15 {
		t.Fatalf("git commands scale with files: %d", gitCalls)
	}
}

func TestParseZ(t *testing.T) {
	t.Run("numstat rename and binary", func(t *testing.T) {
		m := parseNumstatZ("1\t2\ta b.txt\x00-\t-\tbin\x003\t0\t\x00old\x00new\x00")
		testutil.Equal(t, m["a b.txt"].add, 1)
		testutil.Equal(t, m["a b.txt"].del, 2)
		testutil.Equal(t, m["bin"].binary, true)
		testutil.Equal(t, m["new"].orig, "old")
	})
	t.Run("name-status", func(t *testing.T) {
		f := parseNameStatusZ("M\x00a\x00R100\x00o\x00n\x00D\x00z\x00")
		testutil.Equal(t, len(f), 3)
		testutil.Equal(t, f[1].OriginalPath, "o")
		testutil.Equal(t, f[1].Path, "n")
		testutil.Equal(t, f[2].Status, "D")
	})
	t.Run("status collapse", func(t *testing.T) {
		f := parseStatusZ(" M a\x00A  b\x00?? c\x00UU d\x00R  n\x00o\x00")
		var got []string
		for _, x := range f {
			got = append(got, x.Status)
		}
		testutil.DeepEqual(t, got, []string{"M", "A", "??", "U", "R"})
		testutil.Equal(t, f[4].OriginalPath, "o")
	})
}
