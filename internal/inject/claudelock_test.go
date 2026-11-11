package inject

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/inject/atomicfile"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestClaudeConfigLock_RefreshAndOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	testutil.NoError(t, os.WriteFile(path, []byte(`{"token":"keep"}`), 0o600))
	lease, err := acquireClaudeConfigLock(path, time.Second, 5*time.Millisecond)
	testutil.NoError(t, err)
	if lease == nil {
		t.Fatal("missing lease")
	}
	old := time.Now().Add(-time.Hour)
	testutil.NoError(t, os.Chtimes(path+".lock", old, old))
	deadline := time.Now().Add(time.Second)
	refreshed := false
	for time.Now().Before(deadline) {
		info, err := os.Stat(path + ".lock")
		testutil.NoError(t, err)
		if info.ModTime().After(old.Add(time.Minute)) {
			refreshed = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	testutil.True(t, refreshed)
	testutil.NoError(t, lease.check())

	// A successor can replace a compromised lease. Never publish into its
	// critical section or remove its directory when releasing our old inode.
	testutil.NoError(t, os.Rename(path+".lock", path+".old-lock"))
	testutil.NoError(t, os.Mkdir(path+".lock", 0o700))
	testutil.NoError(t, os.Chtimes(path+".lock", old, old))
	testutil.NotNil(t, lease.check())
	testutil.NotNil(t, atomicfile.WriteJSONGuarded(path, map[string]bool{"overwrite": true}, "test", lease.refresh))
	testutil.NotNil(t, lease.close())
	raw, err := os.ReadFile(path)
	testutil.NoError(t, err)
	testutil.Equal(t, string(raw), `{"token":"keep"}`)
	info, err := os.Stat(path + ".lock")
	testutil.NoError(t, err)
	testutil.True(t, info.ModTime().Before(old.Add(time.Minute)))
	entries, err := os.ReadDir(filepath.Dir(path))
	testutil.NoError(t, err)
	testutil.Equal(t, len(entries), 3) // config, our parked inode, successor's lock
}

func TestClaudeConfigLock_RefreshBeforePublish(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	lease, err := acquireClaudeConfigLock(path, time.Second, time.Hour)
	testutil.NoError(t, err)
	if lease == nil {
		t.Fatal("missing lease")
	}
	defer func() { testutil.NoError(t, lease.close()) }()
	old := time.Now().Add(-time.Hour)
	testutil.NoError(t, os.Chtimes(path+".lock", old, old))
	testutil.NoError(t, atomicfile.WriteJSONGuarded(path, map[string]bool{"trusted": true}, "test", lease.refresh))
	info, err := os.Stat(path + ".lock")
	testutil.NoError(t, err)
	testutil.True(t, info.ModTime().After(old.Add(time.Minute)))
}

func TestClaudeConfigLock_HeartbeatDetectsLoss(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	lease, err := acquireClaudeConfigLock(path, time.Second, time.Millisecond)
	testutil.NoError(t, err)
	if lease == nil {
		t.Fatal("missing lease")
	}
	testutil.NoError(t, os.Remove(path+".lock"))
	select {
	case <-lease.done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not notice lease loss")
	}
	testutil.True(t, lease.lost.Load())
	testutil.NotNil(t, lease.close())
}

func TestClaudeConfigLock_AcquireError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := filepath.Join(os.Getenv("HOME"), "file")
	testutil.NoError(t, os.WriteFile(parent, nil, 0o600))
	_, err := acquireClaudeConfigLock(filepath.Join(parent, "config"), time.Millisecond, time.Second)
	testutil.NotNil(t, err)
}
