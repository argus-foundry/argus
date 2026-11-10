package agent

import (
	"fmt"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ValidateScheduleModel checks the current backend choice without reserving a launch.
func ValidateScheduleModel(sched *model.ScheduledTask, cfg config.Config) error {
	if strings.TrimSpace(sched.Model) == "" {
		return nil
	}
	name := resolveTaskChoice(sched.Backend, sched.Project, cfg, "", false).Backend
	backend, ok := cfg.Backends[name]
	if !ok {
		return fmt.Errorf("backend %q not found in config", name)
	}
	if err := ValidateExplicitModel(name, backend, sched.Model); err != nil {
		uxlog.Log("[model] schedule rejected backend=%q model=%q", name, sched.Model)
		return err
	}
	return nil
}
