package client

import (
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentWorkerPreflightBeforeRPC(t *testing.T) {
	agent.ForceSandboxAvailableForTest(t, true)
	c := &Client{peerProtocol: 13, peerSpawnSurface: 43}
	c.SetResidentWorkerIdentity(func(string) (bool, error) { return true, nil })
	cfg := config.DefaultConfig()
	cfg.Sandbox.Enabled = false
	task := &model.Task{ID: "worker", Backend: "claude", Worktree: t.TempDir()}
	for _, action := range []struct {
		name string
		fn   func() error
	}{
		{"start", func() error { _, err := c.Start(task, cfg, 24, 80, true); return err }},
		{"kick", func() error { return c.KickRerender(task, cfg, 24, 80) }},
		{"width", func() error { return c.KickWidthRerender(task, cfg, 24, 80) }},
		{"revive", func() error { return c.Revive(task, cfg, 24, 80) }},
		{"recycle", func() error { return c.Recycle(task, cfg, 24, 80) }},
	} {
		t.Run(action.name, func(t *testing.T) {
			err := action.fn()
			testutil.True(t, err != nil)
			testutil.Contains(t, err.Error(), "requires the Argus sandbox")
		})
	}
	c.SetResidentWorkerIdentity(func(string) (bool, error) { return false, fmt.Errorf("database down") })
	err := c.checkResidentWorkerSupported(task, cfg)
	testutil.Contains(t, err.Error(), "identity")
	c.SetResidentWorkerIdentity(func(string) (bool, error) { return false, nil })
	testutil.NoError(t, c.checkResidentWorkerSupported(task, cfg))
	agent.ForceSandboxAvailableForTest(t, true)
	cfg.Sandbox.Enabled = true
	c.SetResidentWorkerIdentity(func(string) (bool, error) { return true, nil })
	// Fails if the surface guard is deleted or lowered to < 43.
	testutil.Contains(t, c.checkResidentWorkerSupported(task, cfg).Error(), "restart the supervisor")
	c.peerSpawnSurface = 44
	testutil.NoError(t, c.checkResidentWorkerSupported(task, cfg))
	// No peer info (peerProtocol 0) is allowed through.
	c.peerSpawnSurface, c.peerProtocol = 0, 0
	testutil.NoError(t, c.checkResidentWorkerSupported(task, cfg))
}

// Surface 43 (master's artifact publishing) lacks the unattended launch policy. It must
// remain usable for ordinary tasks but cannot safely launch resident workers.
func TestResidentWorkerSurface43(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(fmt.Sprintf("resident_worker=%t", worker), func(t *testing.T) {
			if worker && !agent.IsSandboxAvailable() {
				t.Skip("resident workers require sandbox-exec")
			}
			c := &Client{peerProtocol: 13, peerSpawnSurface: 43}
			c.SetResidentWorkerIdentity(func(string) (bool, error) { return worker, nil })
			cfg := config.DefaultConfig()
			cfg.Sandbox.Enabled = true
			task := &model.Task{ID: "task", Backend: "claude", Worktree: t.TempDir()}
			err := c.checkResidentWorkerSupported(task, cfg)
			if worker {
				testutil.True(t, err != nil)
				testutil.Contains(t, err.Error(), "restart the supervisor")
			} else {
				testutil.NoError(t, err)
			}
		})
	}
}

// The resident's per-launch MCP token ships at surface 44: an older supervisor
// would launch it unverified, so the client refuses the resident there too.
func TestResidentLaunchRequiresSurface44(t *testing.T) {
	cfg := config.DefaultConfig()
	task := &model.Task{ID: "res", Kind: model.KindResident, Backend: "claude", Worktree: t.TempDir()}
	for _, tc := range []struct {
		name    string
		peer    int
		surface int
		refused bool
	}{
		{"old supervisor", 13, 43, true},
		{"current supervisor", 13, 44, false},
		{"no peer info", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{peerProtocol: tc.peer, peerSpawnSurface: tc.surface}
			c.SetResidentWorkerIdentity(func(string) (bool, error) { return false, nil })
			err := c.checkResidentWorkerSupported(task, cfg)
			if tc.refused {
				testutil.True(t, err != nil)
				testutil.Contains(t, err.Error(), "restart the supervisor")
				return
			}
			testutil.NoError(t, err)
		})
	}
}
