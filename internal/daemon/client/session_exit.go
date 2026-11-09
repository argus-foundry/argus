package client

import (
	"context"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/daemon"
)

// WaitForSessionExit confirms process exit and no queued restart over existing
// RPCs. A disconnected output stream never supplies that confirmation.
func (c *Client) WaitForSessionExit(ctx context.Context, taskID string, expectedPID int) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var info daemon.SessionInfo
		if err := c.sessionExitCall(ctx, "Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: taskID}, &info); err != nil {
			return err
		}
		if !info.Alive {
			var pending daemon.PendingRestartResp
			if err := c.sessionExitCall(ctx, "Daemon.HasPendingRestart", &daemon.TaskIDReq{TaskID: taskID}, &pending); err != nil {
				return err
			}
			if pending.Pending {
				return fmt.Errorf("session restart pending")
			}
			return nil
		}
		if expectedPID != 0 && info.PID != expectedPID {
			return fmt.Errorf("session replaced during stop")
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) sessionExitCall(ctx context.Context, method string, args, reply any) error {
	if c == nil || c.rpc == nil {
		return ErrClientClosed
	}
	done := make(chan error, 1)
	go func() { done <- c.rpc.Call(method, args, reply) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// SessionPresent checks authoritative process and restart state before cleanup.
func (c *Client) SessionPresent(ctx context.Context, taskID string) (bool, error) {
	var info daemon.SessionInfo
	if err := c.sessionExitCall(ctx, "Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: taskID}, &info); err != nil {
		return false, err
	}
	if info.Alive {
		return true, nil
	}
	var pending daemon.PendingRestartResp
	if err := c.sessionExitCall(ctx, "Daemon.HasPendingRestart", &daemon.TaskIDReq{TaskID: taskID}, &pending); err != nil {
		return false, err
	}
	return pending.Pending, nil
}
