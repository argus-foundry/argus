package skills

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// add-coord-reap-triggers: the hera_archive entry names its trigger moments
// and the daemon's ready-to-archive notice.
func TestBuiltinHeraArchiveTriggers(t *testing.T) {
	raw, err := os.ReadFile("builtin/hera/SKILL.md")
	testutil.NoError(t, err)
	for _, rule := range []string{
		"**When to call it**",
		"**A worker reports done**",
		"**Every status check or wake**",
		"`hera_archive(dry_run=true)`",
		"**Before going idle, and before reporting the orchestration finished:**",
		"ready to archive",
		"never archives on its own",
	} {
		t.Run(rule, func(t *testing.T) { testutil.Contains(t, string(raw), rule) })
	}
}
