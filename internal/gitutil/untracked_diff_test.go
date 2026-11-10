package gitutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedDiffPatchParity(t *testing.T) {
	for _, quotePath := range []string{"true", "false"} {
		t.Run(quotePath, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			gitRun(t, dir, "config", "core.quotePath", quotePath)
			testutil.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
			for _, tc := range []struct {
				path string
				data string
				mode os.FileMode
			}{
				{"ordinary.txt", "ordinary\n", 0o600},
				{"file with spaces.txt", "spaces\n", 0o600},
				{"日本語.txt", "unicode\n", 0o600},
				{"nested/deep.txt", "nested\n", 0o600},
				{"executable", "#!/bin/sh\n", 0o700},
				{"binary", "\x00\x01\x02binary", 0o600},
				{"empty", "", 0o600},
			} {
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, tc.path), []byte(tc.data), tc.mode))
			}
			testutil.NoError(t, os.Symlink("ordinary.txt", filepath.Join(dir, "relative-link")))
			testutil.NoError(t, os.Symlink(filepath.Join(dir, "ordinary.txt"), filepath.Join(dir, "absolute-inside-link")))
			testutil.NoError(t, os.Symlink(filepath.Join(dir, "nested"), filepath.Join(dir, "absolute-directory-link")))
			before, err := runGit(dir, "count-objects", "-v")
			testutil.NoError(t, err)
			indexBefore, err := runGit(dir, "ls-files", "--stage")
			testutil.NoError(t, err)
			for _, path := range []string{"ordinary.txt", "file with spaces.txt", "日本語.txt", "nested/deep.txt", "executable", "binary", "empty", "relative-link", "absolute-inside-link", "absolute-directory-link/deep.txt"} {
				t.Run(path, func(t *testing.T) {
					expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", path)
					testutil.NotEqual(t, expected, "")
					got := FetchFileDiff("parity", dir, path, Options{})
					testutil.NoError(t, got.Err)
					testutil.Equal(t, got.Diff, expected)
				})
			}
			after, err := runGit(dir, "count-objects", "-v")
			testutil.NoError(t, err)
			testutil.Equal(t, after, before)
			indexAfter, err := runGit(dir, "ls-files", "--stage")
			testutil.NoError(t, err)
			testutil.Equal(t, indexAfter, indexBefore)
		})
	}
}

func TestUntrackedDiffSnapshotLimit(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	file, err := os.Create(filepath.Join(dir, "oversize"))
	testutil.NoError(t, err)
	testutil.NoError(t, file.Truncate(16<<20+1))
	testutil.NoError(t, file.Close())
	got := FetchFileDiff("limit", dir, "oversize", Options{})
	testutil.Error(t, got.Err)
	testutil.Equal(t, got.Diff, "")
}

func TestUntrackedDiffAttributes(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	testutil.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
	for name, content := range map[string]string{
		".gitattributes":        "*.txt -diff\n",
		"nested/.gitattributes": "*.txt diff\n",
		"root.txt":              "root text\n",
		"nested/file.txt":       "nested text\n",
	} {
		testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	for _, path := range []string{"root.txt", "nested/file.txt", ".gitattributes", "nested/.gitattributes"} {
		t.Run(path, func(t *testing.T) {
			expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", path)
			testutil.NotEqual(t, expected, "")
			got := FetchFileDiff("attributes", dir, path, Options{})
			testutil.NoError(t, got.Err)
			testutil.Equal(t, got.Diff, expected)
		})
	}
}
