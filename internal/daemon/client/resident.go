package client

import (
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/daemon"
)

// ForceRecycleCoordinator routes resident rail triggers through the daemon's
// shared recycle reservation. Call off the UI thread.
func (c *Client) ForceRecycleCoordinator(taskID string) error {
	var resp daemon.StatusResp
	if err := c.call("Daemon.ForceRecycleCoordinator", &daemon.TaskIDReq{TaskID: taskID}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

// ResidentState reads the daemon-owned resident state. Call off the UI thread.
func (c *Client) ResidentState() (daemon.ResidentStateResp, error) {
	var resp daemon.ResidentStateResp
	err := c.call("Daemon.ResidentState", &daemon.Empty{}, &resp)
	return resp, err
}

// ResidentMarkViewed acknowledges replies when the resident pane is focused.
func (c *Client) ResidentMarkViewed() error {
	var resp daemon.StatusResp
	if err := c.call("Daemon.ResidentMarkViewed", &daemon.Empty{}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

// ResidentMoveAccount moves the resident to account ("auto", "default" or a
// configured name) and returns the result. Call off the UI thread.
func (c *Client) ResidentMoveAccount(account string, force bool) (daemon.ResidentMoveAccountResp, error) {
	var resp daemon.ResidentMoveAccountResp
	if err := c.call("Daemon.ResidentMoveAccount", &daemon.ResidentMoveAccountReq{Account: account, Force: force}, &resp); err != nil {
		return resp, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// ResidentSend queues an idle-gated message; a nonempty deliveryID deduplicates it.
func (c *Client) ResidentSend(text, deliveryID string) error {
	var resp daemon.StatusResp
	if err := c.call("Daemon.ResidentSend", &daemon.ResidentSendReq{Text: text, DeliveryID: deliveryID}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
