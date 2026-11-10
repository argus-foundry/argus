package agent

import (
	"fmt"
	"log/slog"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ResidentWorkerIdentity must query db.IsResidentWorker at the moment of use;
// task_meta alone is insufficient after hide/unhide. Each PTY owner receives
// its own database-backed lookup, including the supervisor.
type ResidentWorkerIdentity func(string) (bool, error)

func residentWorkerPreflight(task *model.Task, cfg config.Config, identity ...ResidentWorkerIdentity) (bool, error) {
	if len(identity) == 0 || identity[0] == nil {
		return false, nil
	}
	worker, err := identity[0](task.ID)
	if err != nil {
		slog.Warn("[resident] worker identity lookup failed", "task", task.ID, "err", err)
		return false, fmt.Errorf("resident worker identity: %w", err)
	}
	if !worker {
		return false, nil
	}
	err = preflightUnattended(task, cfg)
	if err != nil {
		uxlog.Log("[resident] worker launch refused task=%s: %v", task.ID, err)
		slog.Warn("[resident] worker launch refused", "task", task.ID, "err", err)
	}
	return worker, err
}

// PreflightResidentWorkerLaunch applies the same fail-closed launch gate
// before a remote restart is allowed to stop the existing session.
func PreflightResidentWorkerLaunch(task *model.Task, cfg config.Config, identity ResidentWorkerIdentity) (bool, error) {
	return residentWorkerPreflight(task, cfg, identity)
}

func preflightUnattended(task *model.Task, cfg config.Config) error {
	if !argusWrapsSandbox(task, cfg) {
		return fmt.Errorf("resident worker requires the Argus sandbox")
	}
	b, err := ResolveBackend(task, cfg)
	if err != nil {
		return err
	}
	if FamilyOf(b.Command).UnattendedWorker == nil {
		return fmt.Errorf("backend %q is ineligible for unattended resident workers", task.Backend)
	}
	_, err = FamilyOf(b.Command).UnattendedWorker(b.Command)
	return err
}
