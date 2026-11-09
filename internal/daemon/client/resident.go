package client

import (
	"fmt"

	"github.com/argus-foundry/argus/internal/daemon"
)

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
