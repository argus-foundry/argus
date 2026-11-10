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
	c := &Client{peerProtocol: 13, peerSpawnSurface: 41}
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
	if agent.IsSandboxAvailable() {
		cfg.Sandbox.Enabled = true
		c.SetResidentWorkerIdentity(func(string) (bool, error) { return true, nil })
		testutil.Contains(t, c.checkResidentWorkerSupported(task, cfg).Error(), "restart the supervisor")
		c.peerSpawnSurface = 42
		testutil.NoError(t, c.checkResidentWorkerSupported(task, cfg))
	}
}
