package gitutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestDiscardParentSwap(t *testing.T) {
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	repo := discardRepo(t)
	outside, bin := t.TempDir(), t.TempDir()
	write(t, repo, "sub/victim", "inside")
	write(t, outside, "victim", "outside")
	gitRun(t, repo, "add", "sub/victim")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	shim := fmt.Sprintf("#!/bin/sh\n%s \"$@\"\nrc=$?\nif [ \"$2\" = rm ] && [ $rc = 0 ]; then\n /bin/mv %s %s\n /bin/ln -s %s %s\nfi\nexit $rc\n", quote(realGit), quote(filepath.Join(repo, "sub")), quote(filepath.Join(repo, "saved")), quote(outside), quote(filepath.Join(repo, "sub")))
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	err = DiscardFile(repo, "sub/victim", true)
	testutil.Equal(t, err != nil, true)
	testutil.Equal(t, read(t, outside, "victim"), "outside")
}

func TestDiscardTrackedParentSwap(t *testing.T) {
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	repo := discardRepo(t)
	outside, bin := t.TempDir(), t.TempDir()
	write(t, repo, "sub/c.txt", "edit")
	write(t, outside, "c.txt", "outside")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	shim := fmt.Sprintf("#!/bin/sh\n%s \"$@\"\nrc=$?\nif [ \"$2\" = cat-file ]; then\n /bin/mv %s %s\n /bin/ln -s %s %s\nfi\nexit $rc\n", quote(realGit), quote(filepath.Join(repo, "sub")), quote(filepath.Join(repo, "saved")), quote(outside), quote(filepath.Join(repo, "sub")))
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	testutil.Equal(t, discardCode(DiscardFile(repo, "sub/c.txt", false)), DiscardInvalidPath)
	testutil.Equal(t, read(t, outside, "c.txt"), "outside")
}
