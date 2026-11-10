package gitutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestFetchFileDiffUnborn(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "untracked"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			gitRun(t, dir, "init", "-b", "master")
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "first.txt"), []byte("FIRST-CONTENT\n"), 0o600))
			if staged {
				gitRun(t, dir, "add", "first.txt")
			}
			got := FetchFileDiff("unborn", dir, "first.txt", Options{})
			testutil.NoError(t, got.Err)
			testutil.Contains(t, got.Diff, "new file mode")
			testutil.Contains(t, got.Diff, "@@")
			testutil.Contains(t, got.Diff, "+FIRST-CONTENT\n")
		})
	}
	t.Run("invalid index reports an error", func(t *testing.T) {
		dir := t.TempDir()
		gitRun(t, dir, "init", "-b", "master")
		testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("invalid index"), 0o600))
		got := FetchFileDiff("invalid-index", dir, "first.txt", Options{})
		testutil.Error(t, got.Err)
		testutil.Equal(t, got.Diff, "")
	})
}

func TestFetchFileDiffNonRepo(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "existing directory"
		if missing {
			name = "missing directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("not a repo\n"), 0o600))
			if missing {
				dir = filepath.Join(dir, "missing")
			}
			for _, path := range []string{"plain.txt", "missing.txt"} {
				t.Run(path, func(t *testing.T) {
					got := FetchFileDiff("invalid", dir, path, Options{})
					testutil.Error(t, got.Err)
					testutil.Equal(t, got.Diff, "")
				})
			}
		})
	}
}

func TestFetchFileDiffNoIndexSafety(t *testing.T) {
	t.Run("absolute operands are refused", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		gitRun(t, dir, "checkout", "--detach")
		gitRun(t, dir, "branch", "-D", "master")
		outside := filepath.Join(t.TempDir(), "outside.txt")
		testutil.NoError(t, os.WriteFile(outside, []byte("ABSOLUTE-OUTSIDE\n"), 0o600))
		decoy := filepath.Join(dir, outside)
		testutil.NoError(t, os.MkdirAll(filepath.Dir(decoy), 0o700))
		testutil.NoError(t, os.WriteFile(decoy, []byte("inside decoy\n"), 0o600))
		got := FetchFileDiff("absolute", dir, outside, Options{})
		testutil.NoError(t, got.Err)
		testutil.Equal(t, got.Diff, "")
	})

	t.Run("option operands cannot create or truncate output", func(t *testing.T) {
		for _, exists := range []bool{false, true} {
			name := "absent victim"
			if exists {
				name = "existing victim"
			}
			t.Run(name, func(t *testing.T) {
				dir := initRepo(t, t.TempDir())
				victim := filepath.Join(t.TempDir(), "victim")
				if exists {
					testutil.NoError(t, os.WriteFile(victim, []byte("KEEP\n"), 0o600))
				}
				path := "--output=" + victim
				literal := filepath.Join(dir, path)
				testutil.NoError(t, os.MkdirAll(filepath.Dir(literal), 0o700))
				testutil.NoError(t, os.WriteFile(literal, []byte("literal filename\n"), 0o600))
				got := FetchFileDiff("option", dir, path, Options{})
				testutil.NoError(t, got.Err)
				if exists {
					data, err := os.ReadFile(victim)
					testutil.NoError(t, err)
					testutil.Equal(t, string(data), "KEEP\n")
				} else {
					_, err := os.Stat(victim)
					testutil.ErrorIs(t, err, os.ErrNotExist)
				}
				testutil.Contains(t, got.Diff, "+literal filename\n")
			})
		}
	})

	t.Run("directory symlink outside worktree is refused", func(t *testing.T) {
		for _, unborn := range []bool{false, true} {
			name := "with HEAD"
			if unborn {
				name = "unborn"
			}
			t.Run(name, func(t *testing.T) {
				parent := t.TempDir()
				dir := filepath.Join(parent, "repo")
				testutil.NoError(t, os.Mkdir(dir, 0o700))
				if unborn {
					gitRun(t, dir, "init", "-b", "master")
				} else {
					initRepo(t, dir)
				}
				outside := filepath.Join(parent, "repo-sibling")
				testutil.NoError(t, os.Mkdir(outside, 0o700))
				testutil.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("OUTSIDE-CONTENT\n"), 0o600))
				testutil.NoError(t, os.Symlink(outside, filepath.Join(dir, "escape")))
				got := FetchFileDiff("escape", dir, "escape/secret.txt", Options{})
				testutil.NoError(t, got.Err)
				testutil.Equal(t, got.Diff, "")
			})
		}
	})

	t.Run("contained paths retain added patches", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		testutil.NoError(t, os.WriteFile(filepath.Join(dir, "file with spaces.txt"), []byte("SPACE-CONTENT\n"), 0o600))
		testutil.NoError(t, os.Symlink("file with spaces.txt", filepath.Join(dir, "inside")))
		rootLink := filepath.Join(t.TempDir(), "worktree")
		testutil.NoError(t, os.Symlink(dir, rootLink))
		for _, path := range []string{"file with spaces.txt", "inside", "missing.txt"} {
			t.Run(path, func(t *testing.T) {
				got := FetchFileDiff("contained", rootLink, path, Options{})
				testutil.NoError(t, got.Err)
				if path == "missing.txt" {
					testutil.Equal(t, got.Diff, "")
					return
				}
				testutil.Contains(t, got.Diff, "new file mode")
				testutil.Contains(t, got.Diff, "@@")
				if path == "inside" {
					testutil.Contains(t, got.Diff, "+file with spaces.txt")
				} else {
					testutil.Contains(t, got.Diff, "+SPACE-CONTENT\n")
				}
			})
		}
	})
}
