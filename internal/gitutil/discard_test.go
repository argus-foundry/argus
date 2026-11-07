package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	testutil.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	testutil.NoError(t, err)
	return string(b)
}

func discardCode(err error) string {
	var de *DiscardError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}

func discardRepo(t *testing.T) string {
	dir := initRepo(t, t.TempDir())
	write(t, dir, "a.txt", "a\n")
	write(t, dir, "b.txt", "b\n")
	write(t, dir, "sub/c.txt", "c\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestDiscardFile(t *testing.T) {
	t.Run("restores unstaged and staged edits only for that path", func(t *testing.T) {
		dir := discardRepo(t)
		write(t, dir, "a.txt", "unstaged\n")
		write(t, dir, "b.txt", "staged\n")
		gitRun(t, dir, "add", "b.txt")
		write(t, dir, "b.txt", "staged+unstaged\n")
		testutil.NoError(t, DiscardFile(dir, "b.txt", false))
		testutil.Equal(t, read(t, dir, "b.txt"), "b\n")
		testutil.Equal(t, read(t, dir, "a.txt"), "unstaged\n")
		out, _ := runGit(dir, "status", "--short")
		testutil.Equal(t, out, " M a.txt\n")
	})
	t.Run("restores deleted tracked file", func(t *testing.T) {
		dir := discardRepo(t)
		testutil.NoError(t, os.Remove(filepath.Join(dir, "sub/c.txt")))
		testutil.NoError(t, DiscardFile(dir, "sub/c.txt", false))
		testutil.Equal(t, read(t, dir, "sub/c.txt"), "c\n")
	})
	t.Run("untracked needs explicit flag", func(t *testing.T) {
		dir := discardRepo(t)
		write(t, dir, "new.txt", "n\n")
		testutil.Equal(t, discardCode(DiscardFile(dir, "new.txt", false)), DiscardUntrackedNeedsOK)
		_, err := os.Stat(filepath.Join(dir, "new.txt"))
		testutil.NoError(t, err)
		testutil.NoError(t, DiscardFile(dir, "new.txt", true))
		_, err = os.Stat(filepath.Join(dir, "new.txt"))
		testutil.Equal(t, os.IsNotExist(err), true)
	})
	t.Run("staged new file is removed only with flag", func(t *testing.T) {
		dir := discardRepo(t)
		write(t, dir, "added.txt", "x\n")
		gitRun(t, dir, "add", "added.txt")
		testutil.Equal(t, discardCode(DiscardFile(dir, "added.txt", false)), DiscardUntrackedNeedsOK)
		testutil.NoError(t, DiscardFile(dir, "added.txt", true))
		out, _ := runGit(dir, "status", "--short")
		testutil.Equal(t, out, "")
	})
	t.Run("clean path has no changes", func(t *testing.T) {
		dir := discardRepo(t)
		testutil.Equal(t, discardCode(DiscardFile(dir, "a.txt", false)), DiscardNoChanges)
	})
	t.Run("rename pair refused on either side", func(t *testing.T) {
		dir := discardRepo(t)
		gitRun(t, dir, "mv", "a.txt", "renamed.txt")
		testutil.Equal(t, discardCode(DiscardFile(dir, "a.txt", true)), DiscardRename)
		testutil.Equal(t, discardCode(DiscardFile(dir, "renamed.txt", true)), DiscardRename)
		testutil.Equal(t, read(t, dir, "renamed.txt"), "a\n")
	})
	t.Run("merge conflict refused", func(t *testing.T) {
		dir := discardRepo(t)
		gitRun(t, dir, "checkout", "-q", "-b", "other")
		write(t, dir, "a.txt", "other\n")
		gitRun(t, dir, "commit", "-qam", "other")
		gitRun(t, dir, "checkout", "-q", "-")
		write(t, dir, "a.txt", "main\n")
		gitRun(t, dir, "commit", "-qam", "main")
		_, _ = runGit(dir, "merge", "other")
		testutil.Equal(t, discardCode(DiscardFile(dir, "a.txt", true)), DiscardConflict)
	})
}

func TestValidateWorktreePath(t *testing.T) {
	dir := discardRepo(t)
	outside := t.TempDir()
	write(t, outside, "secret.txt", "s")
	testutil.NoError(t, os.Symlink(outside, filepath.Join(dir, "linkdir")))
	testutil.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "linkfile")))

	bad := []string{
		"", "/etc/passwd", "../x", "a/../../x", "sub/../a.txt", "./a.txt", "a//b",
		".git/config", "sub/.git/config", ".GIT/HEAD", "sub", "linkdir/secret.txt",
		"linkfile", "a\x00b",
	}
	for _, p := range bad {
		t.Run("rejects "+p, func(t *testing.T) {
			_, err := ValidateWorktreePath(dir, p)
			testutil.Equal(t, discardCode(err), DiscardInvalidPath)
		})
	}
	t.Run("accepts nested missing file", func(t *testing.T) {
		_, err := ValidateWorktreePath(dir, "sub/gone.txt")
		testutil.NoError(t, err)
	})
	t.Run("discard of escaping symlink leaves outside file", func(t *testing.T) {
		testutil.Equal(t, discardCode(DiscardFile(dir, "linkdir/secret.txt", true)), DiscardInvalidPath)
		testutil.Equal(t, read(t, outside, "secret.txt"), "s")
	})
}

func TestAbsoluteGitDir(t *testing.T) {
	dir := discardRepo(t)
	gd, err := AbsoluteGitDir(dir)
	testutil.NoError(t, err)
	real, _ := filepath.EvalSymlinks(filepath.Join(dir, ".git"))
	got, _ := filepath.EvalSymlinks(gd)
	testutil.Equal(t, got, real)
	_, err = AbsoluteGitDir(t.TempDir())
	testutil.Equal(t, err != nil, true)
}

func TestParsePorcelainZ(t *testing.T) {
	es := parsePorcelainZ("R  new.txt\x00old.txt\x00?? u.txt\x00 M m.txt\x00")
	testutil.Equal(t, len(es), 3)
	testutil.Equal(t, es[0].orig, "old.txt")
	testutil.Equal(t, es[1].path, "u.txt")
}
