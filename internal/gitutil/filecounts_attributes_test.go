package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestFileCountsAttributesFailure(t *testing.T) {
	for _, command := range []string{"check-attr", "config"} {
		t.Run(command, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			writeFile(t, dir, "new", "one\n")
			realGit, err := exec.LookPath("git")
			testutil.NoError(t, err)
			bin := t.TempDir()
			shim := "#!/bin/sh\nif [ \"$2\" = " + quoteGitCommand(command) + " ]; then exit 128; fi\nexec " + quoteGitCommand(realGit) + " \"$@\"\n"
			testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			wantUnavailableCount(t, byPath(collectFileChanges(dir, ""))["new"])
		})
	}
}

func TestFileCountsUntrackedAttributePolicy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f", "raw\n")
	t.Run("binary before conversion", func(t *testing.T) {
		got := countUntrackedAttributes(dir, FileChange{Path: "f"}, fileCountAttributes{diff: "unset", conversion: true})
		testutil.Equal(t, got.Kind, FileKindBinary)
		testutil.Nil(t, got.Additions)
		testutil.Nil(t, got.Deletions)
	})
	t.Run("unsupported driver", func(t *testing.T) {
		wantUnavailableCount(t, countUntrackedAttributes(dir, FileChange{Path: "f"}, fileCountAttributes{diff: "custom"}))
	})
	t.Run("symlink before conversion", func(t *testing.T) {
		testutil.NoError(t, os.Symlink("missing", filepath.Join(dir, "link")))
		wantCounts(t, countUntrackedAttributes(dir, FileChange{Path: "link"}, fileCountAttributes{textconv: true, conversion: true}), 1, 0)
	})
	t.Run("missing root", func(t *testing.T) {
		wantUnavailableCount(t, countUntracked(filepath.Join(dir, "absent"), FileChange{Path: "f"}))
	})
	t.Run("escaping relative path", func(t *testing.T) { wantUnavailableCount(t, countUntracked(dir, FileChange{Path: "../outside"})) })
}
