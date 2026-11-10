package gitutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedSymlinkPatchParity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mnemonic string
		noprefix string
		color    string
		prefix   string
		filePath string
	}{
		{"mnemonic", "true", "false", "false", "", "link.txt"},
		{"no prefix", "on", "yes", "always", "LEFT/", "link.txt"},
		{"custom prefix", "false", "false", "always", "LEFT/", "new file mode 100644.txt"},
		{"cacheinfo comma", "false", "false", "false", "", "comma, name.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			gitRun(t, dir, "config", "diff.mnemonicPrefix", tc.mnemonic)
			gitRun(t, dir, "config", "diff.noprefix", tc.noprefix)
			gitRun(t, dir, "config", "color.ui", tc.color)
			gitRun(t, dir, "config", "core.autocrlf", "true")
			if tc.prefix != "" {
				gitRun(t, dir, "config", "diff.srcPrefix", tc.prefix)
				gitRun(t, dir, "config", "diff.dstPrefix", "RIGHT/")
			}
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("* text eol=lf -diff\n"), 0o600))
			target := "target\r\nname"
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, target), []byte("target\n"), 0o600))
			testutil.NoError(t, os.Symlink(target, filepath.Join(dir, tc.filePath)))
			expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", tc.filePath)
			testutil.NotEqual(t, expected, "")
			got := FetchFileDiff("link", dir, tc.filePath, Options{})
			testutil.NoError(t, got.Err)
			testutil.Equal(t, got.Diff, expected)
		})
	}
}

func TestCapturedSymlinkLiteralPathspec(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("target\n"), 0o600))
	path := ":(exclude)*.txt"
	testutil.NoError(t, os.Symlink("target", filepath.Join(dir, path)))
	expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", path)
	got, err := untrackedFileDiff(dir, path)
	testutil.NoError(t, err)
	testutil.Equal(t, got, expected)
}

func TestUntrackedSymlinkLeavesExistingObjectUntouched(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("target\n"), 0o600))
	testutil.NoError(t, os.Symlink("target", filepath.Join(dir, "tracked-link")))
	gitRun(t, dir, "add", "tracked-link")
	gitRun(t, dir, "commit", "-m", "tracked link")
	oid, err := runGit(dir, "rev-parse", "HEAD:tracked-link")
	testutil.NoError(t, err)
	oid = strings.TrimSpace(oid)
	object := filepath.Join(dir, ".git", "objects", oid[:2], oid[2:])
	old := time.Unix(1_000_000, 0)
	testutil.NoError(t, os.Chtimes(object, old, old))
	testutil.NoError(t, os.Symlink("target", filepath.Join(dir, "new-link")))
	before, err := runGit(dir, "ls-files", "--stage")
	testutil.NoError(t, err)
	got := FetchFileDiff("existing-blob", dir, "new-link", Options{})
	testutil.NoError(t, got.Err)
	testutil.Contains(t, got.Diff, "new file mode 120000")
	info, err := os.Stat(object)
	testutil.NoError(t, err)
	testutil.Equal(t, info.ModTime().Unix(), old.Unix())
	after, err := runGit(dir, "ls-files", "--stage")
	testutil.NoError(t, err)
	testutil.Equal(t, after, before)
}
