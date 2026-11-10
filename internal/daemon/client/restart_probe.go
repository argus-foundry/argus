package client

import "github.com/argus-foundry/argus/internal/daemon"

// ProbeRestart preserves errors from the existing pending and session RPCs.
func (c *Client) ProbeRestart(taskID string) (live, pending bool, err error) {
	var response daemon.PendingRestartResp
	if err := c.call("Daemon.HasPendingRestart", &daemon.TaskIDReq{TaskID: taskID}, &response); err != nil {
		return false, false, err
	}
	if response.Pending {
		return false, true, nil
	}
	var info daemon.SessionInfo
	if err := c.call("Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: taskID}, &info); err != nil {
		return false, false, err
	}
	return info.Alive, false, nil
}
