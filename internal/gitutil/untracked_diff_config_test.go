package gitutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestDeltaSnapshotRelativeConfig(t *testing.T) {
	for _, kind := range []string{"attributesfile", "textconv"} {
		t.Run(kind, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "data.foo"), []byte("hello\n"), 0o600))
			if kind == "attributesfile" {
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, "custom-attrs"), []byte("*.foo -diff\n"), 0o600))
				gitRun(t, dir, "config", "core.attributesFile", "custom-attrs")
			} else {
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.foo diff=custom\n"), 0o600))
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, "render.sh"), []byte("#!/bin/sh\nprintf 'CONVERTED\\n'\n"), 0o700))
				gitRun(t, dir, "config", "diff.custom.textconv", "./render.sh")
			}
			expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", "data.foo")
			got := FetchFileDiff("delta", dir, "data.foo", Options{})
			t.Logf("native=%q captured=%q err=%v", expected, got.Diff, got.Err)
			testutil.NotEqual(t, expected, "")
			testutil.NoError(t, got.Err)
			testutil.Equal(t, got.Diff, expected)
		})
	}
}

func TestDeltaSnapshotDotSymlink(t *testing.T) {
	for _, prefix := range []string{"default", "noprefix", "mnemonicprefix", "custom"} {
		t.Run(prefix, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			switch prefix {
			case "noprefix", "mnemonicprefix":
				gitRun(t, dir, "config", "diff."+prefix, "true")
			case "custom":
				gitRun(t, dir, "config", "diff.srcPrefix", "old/")
				gitRun(t, dir, "config", "diff.dstPrefix", "new/")
			}
			testutil.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("x\n"), 0o600))
			testutil.NoError(t, os.Symlink("target", filepath.Join(dir, "link")))
			testutil.NoError(t, os.Symlink("../target", filepath.Join(dir, "nested", "link")))
			for _, name := range []string{"./link", "nested/./link", "./target"} {
				t.Run(name, func(t *testing.T) {
					expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", name)
					testutil.NotEqual(t, expected, "")
					got := FetchFileDiff("dot-link", dir, name, Options{})
					testutil.NoError(t, got.Err)
					testutil.Equal(t, got.Diff, expected)
				})
			}
		})
	}
}

func TestUntrackedConverterFailure(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.foo diff=custom\n"), 0o600))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "data.foo"), []byte("payload\n"), 0o600))
	gitRun(t, dir, "config", "diff.custom.textconv", "./missing-converter")
	got := FetchFileDiff("failed-converter", dir, "data.foo", Options{})
	testutil.Error(t, got.Err)
	testutil.Equal(t, got.Diff, "")
}

func TestUntrackedLiteralPathspec(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	name := ":(exclude)*.txt"
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("LITERAL\n"), 0o600))
	got := FetchFileDiff("literal", dir, name, Options{})
	testutil.NoError(t, got.Err)
	testutil.Contains(t, got.Diff, "+LITERAL\n")
	gitRun(t, dir, "--literal-pathspecs", "add", "--", name)
	gitRun(t, dir, "commit", "-m", "literal")
	got = FetchFileDiff("literal", dir, name, Options{})
	testutil.NoError(t, got.Err)
	testutil.Equal(t, got.Diff, "")
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("CHANGED\n"), 0o600))
	got = FetchFileDiff("literal", dir, name, Options{})
	testutil.NoError(t, got.Err)
	testutil.Contains(t, got.Diff, "+CHANGED\n")
}
