//go:build darwin

package api

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWtAddRecountBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("real kqueue Add boundary")
	}
	repo := os.Getenv("WT_FD_REPO")
	if repo == "" {
		wtFDChild(t, wtRepo(t), "boundary", "512")
		return
	}
	root, err := filepath.EvalSymlinks(repo)
	testutil.NoError(t, err)
	// Warm subprocess bookkeeping before measuring watcher descriptors.
	_, err = gitutil.AbsoluteGitDir(root)
	testutil.NoError(t, err)
	h := newWtHub(func(model.Event) {})
	h.budget = &wtWatchBudget{limit: 128}
	h.entryBudget = 64
	walk := h.walk
	h.walk = func(path string, fn fs.WalkDirFunc) error {
		if err := walk(path, fn); err != nil {
			return err
		}
		for i := range 200 {
			if err := os.WriteFile(filepath.Join(root, fmt.Sprint("boundary", i)), nil, 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	e := newDetachedEntry(h, "boundary", repo, gitutil.Options{})
	e.watchRoot = root
	e.watchPaths = make(map[string]bool)
	e.watchDirs = make(map[string]bool)
	testutil.Equal(t, h.budget.reserve(wtBackendUnits()), true)
	e.watchUnits = wtBackendUnits()
	before := wtFDCount(512)
	w, err := newWtWatcher()
	testutil.NoError(t, err)
	e.watchMu.Lock()
	err = e.addTree(w, root)
	charged := e.watchUnits
	e.degradeWatching(w, errors.New("test cleanup"))
	e.watchMu.Unlock()
	after := wtFDCount(512)
	t.Logf("Add return charged=%d allowance=64 fds before=%d after=%d err=%v", charged, before, after, err)
	testutil.Equal(t, errors.Is(err, errWtWatchBudget), true)
	testutil.Equal(t, after <= before, true)
}
