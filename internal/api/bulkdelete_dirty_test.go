package api

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
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBulkDelete_DirtyCancellationKillsDescendants(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	fifo := filepath.Join(bin, "ready")
	testutil.NoError(t, syscall.Mkfifo(fifo, 0600))
	script := "#!/bin/sh\n/bin/sleep 30 &\necho $! > '" + fifo + "'\nwait\n"
	testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := (gitDeleteOps{ctx: ctx}).IsDirty(bin); done <- err }()
	ready, err := os.Open(fifo)
	testutil.NoError(t, err)
	b, err := io.ReadAll(ready)
	testutil.NoError(t, err)
	testutil.NoError(t, ready.Close())
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	testutil.NoError(t, err)
	defer func() {
		err := syscall.Kill(pid, syscall.SIGKILL)
		if !errors.Is(err, syscall.ESRCH) {
			testutil.NoError(t, err)
		}
	}()
	cancel()
	select {
	case err := <-done:
		testutil.True(t, err != nil)
	case <-time.After(3 * time.Second):
		t.Fatal("dirty check did not cancel")
	}
	// The child can briefly be a zombie after group termination, but must not run.
	err = syscall.Kill(pid, 0)
	if err == nil {
		out, readErr := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
		if readErr == nil {
			testutil.True(t, strings.HasPrefix(strings.TrimSpace(string(out)), "Z"))
		}
	}
}
