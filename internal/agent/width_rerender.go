package agent

import (
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

type widthRerenderKicker interface {
	KickWidthRerender(*model.Task, config.Config, uint16, uint16) error
}

// StopRerenderForPID claims the expected process for native width repair.
func StopRerenderForPID(provider SessionProvider, taskID string, expectedPID int) error {
	if owner, ok := provider.(*Runner); ok {
		return owner.stopScoped(taskID, StopRerender, true, expectedPID)
	}
	if owner, ok := provider.(interface{ StopRerenderForPID(string, int) error }); ok {
		return owner.StopRerenderForPID(taskID, expectedPID)
	}
	session, err := AttachableSession(provider, taskID)
	if err != nil {
		return err
	}
	if session == nil || session.PID() != expectedPID {
		return ErrSessionNotFound
	}
	return session.StopScoped(StopRerender)
}

// ResizeSessionForPID resizes a specific process. Proxies must pass the
// expectation through to the process owner instead of resolving by task ID.
func ResizeSessionForPID(session SessionHandle, rows, cols uint16, expectedPID int) error {
	if expectedPID == 0 {
		return session.Resize(rows, cols)
	}
	if scoped, ok := session.(interface {
		ResizeForPID(uint16, uint16, int) error
	}); ok {
		return scoped.ResizeForPID(rows, cols, expectedPID)
	}
	if !session.Alive() || session.PID() != expectedPID {
		return ErrSessionNotFound
	}
	return session.Resize(rows, cols)
}

// KickWidthRerender opts a width-repair caller into bounded stop when its
// provider supports it. Older providers retain the non-escalating behavior.
func KickWidthRerender(r interface {
	KickRerender(*model.Task, config.Config, uint16, uint16) error
}, task *model.Task, cfg config.Config, rows, cols uint16) error {
	if bounded, ok := r.(widthRerenderKicker); ok {
		return bounded.KickWidthRerender(task, cfg, rows, cols)
	}
	return r.KickRerender(task, cfg, rows, cols)
}
