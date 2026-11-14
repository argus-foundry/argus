package client

// startReceipt identifies the most recent Start RPC issued for a task and the
// PID it actually returned. A timeout doesn't cancel that RPC; completion can
// publish the PID later, without racing a caller reading its response buffer.
type startReceipt struct {
	sequence uint64
	pid      int
	pending  bool
}

// StartReceipt returns local launch evidence, including late RPC completion.
// A newer Start invalidates prior evidence even before it returns a PID.
func (c *Client) StartReceipt(taskID string) (uint64, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.starts[taskID]
	return r.sequence, r.pid, r.pending
}
