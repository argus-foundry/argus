package skew

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// RuntimeSource builds the /api/runtime snapshot from the daemon's own BootInfo
// and the installed executable, using the same hash and surface policy as the
// TUI skew evaluation. Any failed step degrades to "unknown", never to stale.
type RuntimeSource struct {
	info      func() (daemon.BootInfoResp, error)
	installed func() (string, error)
}

var _ api.RuntimeSource = (*RuntimeSource)(nil)

func NewRuntimeSource(info func() (daemon.BootInfoResp, error)) *RuntimeSource {
	return &RuntimeSource{info: info, installed: newInstalledHasher(os.Executable)}
}

type hashStamp struct {
	path  string
	size  int64
	mtime time.Time
}

// newInstalledHasher re-hashes the resolved executable only when its path, size
// or mtime changed, so polling /api/runtime does not re-read the binary.
func newInstalledHasher(exe func() (string, error)) func() (string, error) {
	return newInstalledHasherWithHash(exe, daemon.BinaryHashFile)
}

func newInstalledHasherWithHash(exe func() (string, error), hashFile func(string) (string, error)) func() (string, error) {
	var (
		mu    sync.Mutex
		stamp hashStamp
		hash  string
	)
	return func() (string, error) {
		p, err := exe()
		if err != nil {
			return "", err
		}
		if resolved, rerr := filepath.EvalSymlinks(p); rerr == nil {
			p = resolved
		}
		st, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		cur := hashStamp{p, st.Size(), st.ModTime()}
		mu.Lock()
		defer mu.Unlock()
		if hash != "" && cur == stamp {
			return hash, nil
		}
		h, err := hashFile(p)
		if err != nil {
			return "", err
		}
		stamp, hash = cur, h
		return h, nil
	}
}

func (r *RuntimeSource) Snapshot() api.RuntimeSnapshot {
	var snap api.RuntimeSnapshot
	info, err := r.info()
	if err != nil {
		uxlog.Log("[runtime] BootInfo failed: %v", err)
		return snap
	}
	installed, ierr := r.installed()
	if ierr != nil {
		uxlog.Log("[runtime] installed identity unavailable: %v", ierr)
		installed = ""
	}
	snap.DaemonHash = info.BinaryHash
	snap.DaemonBootedAt = info.BootedAt
	snap.InstalledHash = installed
	snap.RestartSupported = info.SupervisorPresent

	snap.DaemonState = "unknown"
	if info.BinaryHash != "" && installed != "" {
		snap.DaemonState = "current"
		if info.BinaryHash != installed {
			snap.DaemonState = "stale"
		}
	}

	snap.SupervisorState, snap.SupervisorCompatibility = "unknown", "unknown"
	if info.SupervisorPresent && info.SupervisorHash != "" {
		snap.SupervisorHash = info.SupervisorHash
		if installed != "" {
			snap.SupervisorState = "current"
			if info.SupervisorHash != installed {
				snap.SupervisorState = "stale"
			}
		}
		switch SupervisorVerdict(info, installed, installed == "") {
		case daemon.SurfaceCoherent:
			snap.SupervisorCompatibility = "compatible"
		case daemon.SurfaceUnknown:
		default:
			snap.SupervisorCompatibility = "incompatible"
		}
	}
	return snap
}
