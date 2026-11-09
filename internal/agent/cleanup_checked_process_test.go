package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func assertStoppedGitChild(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err == nil {
		testutil.True(t, strings.HasPrefix(strings.TrimSpace(string(out)), "Z"))
	}
}

func TestCleanupTaskGit_ExitedLeaderKillsChild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	fifo := filepath.Join(bin, "ready")
	testutil.NoError(t, syscall.Mkfifo(fifo, 0600))
	script := "#!/bin/sh\n/bin/sleep 30 &\necho $! > '" + fifo + "'\nexit 0\n"
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	done := make(chan error, 1)
	go func() { _, _, err := RunTaskGit(context.Background(), bin, "status"); done <- err }()
	ready, err := os.Open(fifo)
	testutil.NoError(t, err)
	b, err := io.ReadAll(ready)
	testutil.NoError(t, err)
	ready.Close()
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	testutil.NoError(t, err)
	defer func() {
		err := syscall.Kill(pid, syscall.SIGKILL)
		if !errors.Is(err, syscall.ESRCH) {
			testutil.NoError(t, err)
		}
	}()
	testutil.ErrorIs(t, <-done, exec.ErrWaitDelay)
	assertStoppedGitChild(t, pid)
}

func TestCleanupTaskGit_HookOutputTimeoutFailsAndKillsChild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, origin := cleanupRepo(t)
	wt, branch := addCleanupWorktree(t, repo, "hook-child", true)
	pidfile := filepath.Join(t.TempDir(), "pid")
	hook := "#!/bin/sh\n/bin/sleep 30 &\necho $! > '" + pidfile + "'\nexit 0\n"
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-push"), []byte(hook), 0700))
	result := CleanupTaskGit(wt, branch, repo)
	raw, err := os.ReadFile(pidfile)
	testutil.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	testutil.NoError(t, err)
	defer func() {
		err := syscall.Kill(pid, syscall.SIGKILL)
		if !errors.Is(err, syscall.ESRCH) {
			testutil.NoError(t, err)
		}
	}()
	assertStoppedGitChild(t, pid)
	testutil.Equal(t, result.Failed(), true)
	testutil.Equal(t, result.RemoteBranch, CleanupFailed)
	// Git may have applied the deletion before the output timeout.
	cmd := exec.Command("git", "--git-dir", origin, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	testutil.True(t, cmd.Run() != nil)
}
