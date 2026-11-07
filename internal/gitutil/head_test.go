package gitutil

import (
	"os/exec"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestReadHead(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		h, err := ReadHead(dir)
		testutil.NoError(t, err)
		testutil.Equal(t, *h.Branch, "master")
		testutil.Equal(t, *h.OID, headSHA(t, dir))
		testutil.Equal(t, h.Detached, false)
	})
	t.Run("checkout follows the live branch", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		gitRun(t, dir, "checkout", "-b", "feature/x")
		h, err := ReadHead(dir)
		testutil.NoError(t, err)
		testutil.Equal(t, *h.Branch, "feature/x")
	})
	t.Run("rename", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		gitRun(t, dir, "branch", "-m", "renamed")
		h, err := ReadHead(dir)
		testutil.NoError(t, err)
		testutil.Equal(t, *h.Branch, "renamed")
	})
	t.Run("detached", func(t *testing.T) {
		dir := initRepo(t, t.TempDir())
		gitRun(t, dir, "checkout", "--detach")
		h, err := ReadHead(dir)
		testutil.NoError(t, err)
		testutil.Nil(t, h.Branch)
		testutil.Equal(t, h.Detached, true)
		testutil.Equal(t, *h.OID, headSHA(t, dir))
	})
	t.Run("unborn", func(t *testing.T) {
		dir := t.TempDir()
		if out, err := exec.Command("git", "init", "-b", "fresh", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
		h, err := ReadHead(dir)
		testutil.NoError(t, err)
		testutil.Equal(t, *h.Branch, "fresh")
		testutil.Nil(t, h.OID)
		testutil.Equal(t, h.Detached, false)
	})
	t.Run("not a repo is an error", func(t *testing.T) {
		_, err := ReadHead(t.TempDir())
		testutil.Equal(t, err != nil, true)
	})
	t.Run("empty worktree is an error", func(t *testing.T) {
		_, err := ReadHead("")
		testutil.Equal(t, err != nil, true)
	})
}

func TestFetchGitStatusHead(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	gitRun(t, dir, "checkout", "-b", "live")
	msg := FetchGitStatus("t", dir, Options{})
	testutil.NoError(t, msg.Err)
	testutil.Equal(t, *msg.Head.Branch, "live")
	testutil.Nil(t, msg.HeadErr)

	bad := FetchGitStatus("t", t.TempDir(), Options{})
	testutil.Equal(t, bad.Err != nil, true)
}
