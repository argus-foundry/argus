package skew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

func src(info daemon.BootInfoResp, ierr error, installed string, installedErr error) *RuntimeSource {
	return &RuntimeSource{
		info:      func() (daemon.BootInfoResp, error) { return info, ierr },
		installed: func() (string, error) { return installed, installedErr },
	}
}

func TestRuntimeSnapshot(t *testing.T) {
	booted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cur := daemon.CurrentSupervisorSurface()
	base := daemon.BootInfoResp{
		BinaryHash: "d1", BootedAt: booted, SupervisorPresent: true, SupervisorHash: "s1",
		SupervisorSpawnSurface: cur.Spawn, SupervisorStreamSurface: cur.Stream,
	}

	t.Run("daemon current supervisor current", func(t *testing.T) {
		b := base
		b.BinaryHash, b.SupervisorHash = "x", "x"
		s := src(b, nil, "x", nil).Snapshot()
		testutil.Equal(t, s.DaemonState, "current")
		testutil.Equal(t, s.SupervisorState, "current")
		testutil.Equal(t, s.SupervisorCompatibility, "compatible")
		testutil.Equal(t, s.RestartSupported, true)
		testutil.Equal(t, s.InstalledHash, "x")
	})
	t.Run("daemon stale; hash-stale supervisor still compatible", func(t *testing.T) {
		s := src(base, nil, "new", nil).Snapshot()
		testutil.DeepEqual(t, s, api.RuntimeSnapshot{
			DaemonHash: "d1", DaemonBootedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			DaemonState: "stale", InstalledHash: "new", SupervisorHash: "s1",
			SupervisorState: "stale", SupervisorCompatibility: "compatible", RestartSupported: true,
		})
		testutil.Equal(t, s.DaemonState, "stale")
		testutil.Equal(t, s.SupervisorState, "stale")
		testutil.Equal(t, s.SupervisorCompatibility, "compatible")
	})
	t.Run("surface mismatch is incompatible", func(t *testing.T) {
		b := base
		b.SupervisorStreamSurface = cur.Stream + 1
		testutil.Equal(t, src(b, nil, "d1", nil).Snapshot().SupervisorCompatibility, "incompatible")
	})
	t.Run("pre-surface supervisor is unknown compatibility", func(t *testing.T) {
		b := base
		b.SupervisorSpawnSurface, b.SupervisorStreamSurface = 0, 0
		b.SupervisorHash = "d1"
		s := src(b, nil, "d1", nil).Snapshot()
		testutil.Equal(t, s.SupervisorCompatibility, "unknown")
		testutil.Equal(t, s.SupervisorState, "current")
	})
	t.Run("missing installed executable is unknown not stale", func(t *testing.T) {
		s := src(base, nil, "", errors.New("gone")).Snapshot()
		testutil.Equal(t, s.DaemonState, "unknown")
		testutil.Equal(t, s.SupervisorState, "unknown")
		testutil.Equal(t, s.InstalledHash, "")
		testutil.Equal(t, s.DaemonHash, "d1")
	})
	t.Run("unreachable supervisor has no hash and unknown", func(t *testing.T) {
		b := base
		b.SupervisorHash = ""
		s := src(b, nil, "d1", nil).Snapshot()
		testutil.Equal(t, s.SupervisorHash, "")
		testutil.Equal(t, s.SupervisorState, "unknown")
		testutil.Equal(t, s.SupervisorCompatibility, "unknown")
		testutil.Equal(t, s.RestartSupported, true)
	})
	t.Run("legacy mode has no supervisor and refuses restart", func(t *testing.T) {
		b := daemon.BootInfoResp{BinaryHash: "d1"}
		s := src(b, nil, "d1", nil).Snapshot()
		testutil.Equal(t, s.RestartSupported, false)
		testutil.Equal(t, s.SupervisorState, "unknown")
		testutil.Equal(t, s.DaemonState, "current")
	})
	t.Run("BootInfo failure is entirely unknown", func(t *testing.T) {
		s := src(daemon.BootInfoResp{}, errors.New("rpc"), "x", nil).Snapshot()
		testutil.Equal(t, s.DaemonState, "")
		testutil.Equal(t, s.RestartSupported, false)
	})
}

func TestInstalledHasherCachesByStat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	testutil.NoError(t, os.WriteFile(p, []byte("one"), 0o755))
	link := filepath.Join(dir, "link")
	testutil.NoError(t, os.Symlink(p, link))
	hashReads := 0
	h := newInstalledHasherWithHash(func() (string, error) { return link, nil }, func(path string) (string, error) {
		hashReads++
		return daemon.BinaryHashFile(path)
	})

	a, err := h()
	testutil.NoError(t, err)
	b, err := h()
	testutil.NoError(t, err)
	testutil.Equal(t, a, b)
	testutil.Equal(t, hashReads, 1)

	testutil.NoError(t, os.WriteFile(p, []byte("two-longer"), 0o755))
	c, err := h()
	testutil.NoError(t, err)
	testutil.Equal(t, hashReads, 2)
	if c == a {
		t.Fatal("hash must change when the binary changes")
	}

	missing := newInstalledHasher(func() (string, error) { return filepath.Join(dir, "none"), nil })
	if _, err := missing(); err == nil {
		t.Fatal("expected stat error")
	}
	failing := newInstalledHasher(func() (string, error) { return "", errors.New("no exe") })
	if _, err := failing(); err == nil {
		t.Fatal("expected exe error")
	}
}
