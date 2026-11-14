//go:build darwin

package api

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"golang.org/x/sys/unix"
)

func wtFDCount(limit uintptr) int {
	n := 0
	for fd := uintptr(0); fd < limit; fd++ {
		if _, err := unix.FcntlInt(fd, unix.F_GETFD, 0); err == nil {
			n++
		}
	}
	return n
}

func wtFDChild(t *testing.T, repo, mode, limit string) {
	t.Helper()
	exe, err := os.Executable()
	testutil.NoError(t, err)
	cmd := exec.Command("/bin/sh", "-c", `ulimit -n "$1"; shift; exec "$@"`, "sh", limit, exe, "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), "WT_FD_REPO="+repo, "WT_FD_MODE="+mode)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	testutil.NoError(t, err)
}

type wtMeterWatcher struct {
	wtWatcher
	afterAdd func(string, error)
}

func (w *wtMeterWatcher) Add(path string) error {
	err := w.wtWatcher.Add(path)
	w.afterAdd(path, err)
	return err
}

func TestWtWatchAddGap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("real kqueue interleaving")
	}
	repo := os.Getenv("WT_FD_REPO")
	if repo == "" {
		wtFDChild(t, wtRepo(t), "gap", "8192")
		return
	}
	root, err := filepath.EvalSymlinks(repo)
	testutil.NoError(t, err)
	h := newWtHub(func(model.Event) {})
	defer h.closeAll()
	h.budget = &wtWatchBudget{limit: 256}
	h.entryBudget = 128
	walk := h.walk
	h.walk = func(path string, fn fs.WalkDirFunc) error {
		if err := walk(path, fn); err != nil {
			return err
		}
		for i := range 5000 {
			if err := os.WriteFile(filepath.Join(root, fmt.Sprint("late", i)), nil, 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	_, err = exec.Command("/usr/bin/true").CombinedOutput()
	testutil.NoError(t, err)
	before := wtFDCount(8192)
	peak := 0
	h.newWatcher = func() (wtWatcher, error) {
		w, err := newWtWatcher()
		if err != nil {
			return nil, err
		}
		return &wtMeterWatcher{w, func(path string, err error) {
			if path == root {
				testutil.NoError(t, err)
				peak = wtFDCount(8192)
			}
		}}, nil
	}
	e := h.register("gap", repo, gitutil.Options{})
	<-e.watchReady
	<-e.refresh("")
	e.mu.Lock()
	degraded := e.degraded
	e.mu.Unlock()
	h.remove("gap")
	after := wtFDCount(8192)
	t.Logf("Add gap fds before=%d peak=%d after=%d degraded=%t reservations=%d", before, peak, after, degraded, wtBudgetUsed(t, h.budget))
	testutil.Equal(t, degraded, true)
	testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
	testutil.Equal(t, after <= before, true)
}

func TestWtDescriptorCycles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("real kqueue descriptor cycles")
	}
	repo := os.Getenv("WT_FD_REPO")
	mode := os.Getenv("WT_FD_MODE")
	if repo == "" {
		for _, kind := range []string{"normal", "degraded"} {
			t.Run(kind, func(t *testing.T) {
				repo := wtRepo(t)
				if kind == "degraded" {
					for i := range 300 {
						testutil.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprint("file", i)), nil, 0o600))
					}
				}
				// Run only the parent test so the child reads the mode without spawning again.
				exe, err := os.Executable()
				testutil.NoError(t, err)
				cmd := exec.Command("/bin/sh", "-c", `ulimit -n 128; exec "$@"`, "sh", exe, "-test.run=^TestWtDescriptorCycles$", "-test.v")
				cmd.Env = append(os.Environ(), "WT_FD_REPO="+repo, "WT_FD_MODE="+kind)
				output, err := cmd.CombinedOutput()
				t.Log(string(output))
				testutil.NoError(t, err)
			})
		}
		return
	}
	h := newWtHub(func(model.Event) {})
	defer h.closeAll()
	testutil.NoError(t, exec.Command("/usr/bin/true").Run())
	before := wtFDCount(128)
	t.Logf("mode=%s process budget=%d fds before=%d", mode, h.budget.limit, before)
	for i := range 30 {
		id := fmt.Sprint("cycle", i)
		e := h.register(id, repo, gitutil.Options{})
		<-e.watchReady
		<-e.refresh("")
		e.mu.Lock()
		degraded := e.degraded
		e.mu.Unlock()
		testutil.Equal(t, degraded, mode == "degraded")
		h.remove(id)
		now := wtFDCount(128)
		if err := exec.Command("/usr/bin/true").Run(); err != nil {
			t.Fatalf("cycle %d fds=%d budget.used=%d subprocess failed: %v", i, now, wtBudgetUsed(t, h.budget), err)
		}
	}
	after := wtFDCount(128)
	for deadline := time.Now().Add(time.Second); after > before && time.Now().Before(deadline); {
		runtime.Gosched()
		after = wtFDCount(128)
	}
	t.Logf("mode=%s 30 cycles fds before=%d after=%d budget.used=%d", mode, before, after, wtBudgetUsed(t, h.budget))
	testutil.Equal(t, after <= before, true)
	testutil.Equal(t, wtBudgetUsed(t, h.budget), 0)
}

func TestWtPartialAddCleanup(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("real kqueue partial Add")
	}
	repo := os.Getenv("WT_FD_REPO")
	if repo == "" {
		repo = wtRepo(t)
		for i := range 300 {
			testutil.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprint("partial", i)), nil, 0o600))
		}
		wtFDChild(t, repo, "partial", "128")
		return
	}
	_, err := exec.Command("/usr/bin/true").CombinedOutput()
	testutil.NoError(t, err)
	before := wtFDCount(128)
	w, err := newWtWatcher()
	testutil.NoError(t, err)
	err = w.Add(repo)
	testutil.Equal(t, err != nil, true)
	testutil.NoError(t, w.Close())
	after := wtFDCount(128)
	testutil.NoError(t, exec.Command("/usr/bin/true").Run())
	t.Logf("partial Add fds before=%d after=%d", before, after)
	testutil.Equal(t, after <= before, true)
}
