package daemon

import (
	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/hera"
)

// heraArchiveRunner adapts the daemon's runner to hera.ArchiveRunner
// (add-hera-auto-archive). Liveness/idle/prompt checks reuse HeraReviveRunner.
type heraArchiveRunner struct {
	*HeraReviveRunner
	runner agent.SessionRunner
}

var _ hera.ArchiveRunner = (*heraArchiveRunner)(nil)

func (r *heraArchiveRunner) Stop(taskID string) error { return r.runner.Stop(taskID) }

// heraArchiver returns the archive seams for mcp.Server.SetHeraArchiver.
func (d *Daemon) heraArchiver() (hera.ArchiveRunner, hera.GitCheck, hera.HandoffCheck) {
	return &heraArchiveRunner{HeraReviveRunner: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner}, hera.NewGitCheck(d.cfgFn), hera.NewHandoffCheck()
}
