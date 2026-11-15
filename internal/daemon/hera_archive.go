package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
)

// heraArchiveRunner adapts the daemon's runner to hera.ArchiveRunner
// (add-hera-auto-archive). Liveness/idle/prompt checks reuse HeraReviveRunner.
type heraArchiveRunner struct {
	*HeraReviveRunner
	runner agent.SessionRunner
}

var _ hera.ArchiveRunner = (*heraArchiveRunner)(nil)

func (r *heraArchiveRunner) Stop(taskID string) error { return r.runner.Stop(taskID) }

// heraArchiveGitCheck is the production hera.GitCheck: the merge-safety
// classifier must confirm the branch landed AND the worktree must be clean.
// Every unknown fails closed.
func heraArchiveGitCheck(cfgFn func() config.Config) hera.GitCheck {
	return func(t *model.Task) (bool, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cfg := cfgFn()
		v, err := mergesafety.ClassifyTask(ctx, cfg, t)
		if err != nil {
			return false, fmt.Sprintf("merge-safety error: %v", err)
		}
		if !v.Safe {
			return false, v.Reason
		}
		if t.Worktree != "" {
			if s := mergesafety.SummarizeTask(ctx, cfg, t, ""); s.DirtyCount != "0" {
				return false, "worktree has uncommitted changes (" + s.DirtyCount + ")"
			}
		}
		return true, ""
	}
}

// heraArchiver returns the archive seams for mcp.Server.SetHeraArchiver.
func (d *Daemon) heraArchiver() (hera.ArchiveRunner, hera.GitCheck) {
	return &heraArchiveRunner{HeraReviveRunner: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner}, heraArchiveGitCheck(d.cfgFn)
}
