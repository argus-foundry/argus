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
	worker, err := agent.PreflightResidentWorkerLaunch(task, cfg, identity)
	if err != nil {
		return err
	}
	if worker && peer != 0 && surface < 43 {
		uxlog.Log("[resident] refusing worker task=%s: supervisor spawn surface %d < 43", task.ID, surface)
		return fmt.Errorf("resident worker requires a supervisor with unattended-worker support; restart the supervisor and retry")
	}
	return nil
}
