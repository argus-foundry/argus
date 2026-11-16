package hera

import (
	"context"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
)

// NewGitCheck is the production GitCheck: the merge-safety classifier must
// confirm the branch landed AND the worktree must be clean. Every unknown
// fails closed. Shared by the daemon (hera_archive, REST) and the local TUI.
func NewGitCheck(cfgFn func() config.Config) GitCheck {
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

// NewHandoffCheck is the production HandoffCheck (design D2): PR open and not
// failing CI, pushed tip == local tip == PR head, clean worktree.
func NewHandoffCheck() HandoffCheck {
	return func(t *model.Task) (HandoffInfo, bool, string) {
		st, ok, reason := gitutil.CheckHandoff(context.Background(), t.Worktree, t.Branch)
		if !ok {
			return HandoffInfo{}, false, reason
		}
		return HandoffInfo{PRURL: st.PRURL, TipSHA: st.TipSHA}, true, ""
	}
}
