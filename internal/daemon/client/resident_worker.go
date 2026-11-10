package client

import (
	"fmt"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// SetResidentWorkerIdentity is wired by the daemon before serving requests.
func (c *Client) SetResidentWorkerIdentity(identity agent.ResidentWorkerIdentity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.residentWorker = identity
}

func (c *Client) checkResidentWorkerSupported(task *model.Task, cfg config.Config) error {
	c.mu.Lock()
	identity, peer, surface := c.residentWorker, c.peerProtocol, c.peerSpawnSurface
	c.mu.Unlock()
	if task.IsResident() && peer != 0 && surface < 44 {
		// Surface 44 delivers the resident's per-launch MCP token; an older
		// supervisor would launch it unverified (no delete/graduate/recycle).
		uxlog.Log("[resident] refusing resident task=%s: supervisor spawn surface %d < 44", task.ID, surface)
		return fmt.Errorf("the resident requires a supervisor with per-launch identity support; restart the supervisor and retry")
	}
	worker, err := agent.PreflightResidentWorkerLaunch(task, cfg, identity)
	if err != nil {
		return err
	}
	if worker && peer != 0 && surface < 44 {
		uxlog.Log("[resident] refusing worker task=%s: supervisor spawn surface %d < 44", task.ID, surface)
		return fmt.Errorf("resident worker requires a supervisor with unattended-worker support; restart the supervisor and retry")
	}
	return nil
}
