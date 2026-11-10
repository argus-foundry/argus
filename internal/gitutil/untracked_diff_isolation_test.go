package gitutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedDiffConcurrentIsolation(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "regular"), []byte("CONTENT\n"), 0700))
	testutil.NoError(t, os.Symlink("regular", filepath.Join(dir, "link")))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "info", "attributes"), []byte("regular diff=custom\n"), 0o600))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "render.sh"), []byte("#!/bin/sh\ncat \"$1\"\n"), 0o700))
	gitRun(t, dir, "config", "diff.custom.textconv", "./render.sh")
	gitRun(t, dir, "config", "diff.custom.cachetextconv", "true")
	refsBefore, err := runGit(dir, "for-each-ref", "--format=%(refname) %(objectname)")
	testutil.NoError(t, err)
	indexBefore, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	testutil.NoError(t, err)
	objectsBefore, err := runGit(dir, "count-objects", "-v")
	testutil.NoError(t, err)
	expected := map[string]string{}
	for _, name := range []string{"regular", "link"} {
		expected[name], _ = runGit(dir, "diff", "--no-index", "--", "/dev/null", name)
	}
	type result struct {
		name, diff string
		err        error
	}
	done := make(chan result, 12)
	for i := 0; i < 12; i++ {
		name := "regular"
		if i%2 == 0 {
			name = "link"
		}
		go func(name string) {
			got := FetchFileDiff("concurrent", dir, name, Options{})
			done <- result{name, got.Diff, got.Err}
		}(name)
	}
	for i := 0; i < 12; i++ {
		got := <-done
		testutil.NoError(t, got.err)
		testutil.Equal(t, got.diff, expected[got.name])
	}
	indexAfter, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	testutil.NoError(t, err)
	testutil.Equal(t, string(indexAfter), string(indexBefore))
	objectsAfter, err := runGit(dir, "count-objects", "-v")
	testutil.NoError(t, err)
	testutil.Equal(t, objectsAfter, objectsBefore)
	refsAfter, err := runGit(dir, "for-each-ref", "--format=%(refname) %(objectname)")
	testutil.NoError(t, err)
	testutil.Equal(t, refsAfter, refsBefore)
	large, err := os.Create(filepath.Join(dir, "large"))
	testutil.NoError(t, err)
	testutil.NoError(t, large.Truncate(maxUntrackedDiffBytes))
	testutil.NoError(t, large.Close())
	got := FetchFileDiff("boundary", dir, "large", Options{})
	testutil.NoError(t, got.Err)
	testutil.Contains(t, got.Diff, "Binary files")
	large, err = os.OpenFile(filepath.Join(dir, "large"), os.O_WRONLY, 0600)
	testutil.NoError(t, err)
	testutil.NoError(t, large.Truncate(maxUntrackedDiffBytes+1))
	testutil.NoError(t, large.Close())
	got = FetchFileDiff("overflow", dir, "large", Options{})
	testutil.Error(t, got.Err)
	testutil.Equal(t, got.Diff, "")
	entries, err := os.ReadDir(temporary)
	testutil.NoError(t, err)
	testutil.Equal(t, len(entries), 0)
}
