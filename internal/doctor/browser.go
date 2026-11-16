package doctor

import (
	"fmt"
	"strings"

	"github.com/argus-foundry/argus/internal/browserproc"
)

// RenderBrowserOrphans is advisory-only. The caller supplies the outcome of
// read-only process inspection; no browser is signaled by doctor.
func RenderBrowserOrphans(procs []browserproc.Process, err error) string {
	var b strings.Builder
	b.WriteString("\nOrphaned Argus Playwright browsers: ")
	if err != nil {
		return b.String() + "UNKNOWN (process inspection unavailable)\n"
	}
	var orphans []browserproc.Process
	for _, p := range procs {
		if p.Root && p.PPID == 1 {
			orphans = append(orphans, p)
		}
	}
	if len(orphans) == 0 {
		return b.String() + "NONE FOUND\n"
	}
	fmt.Fprintf(&b, "FOUND (%d)\n", len(orphans))
	for _, p := range orphans {
		fmt.Fprintf(&b, "  pid %d  %s (parent is PID 1)\n", p.PID, p.Worktree)
	}
	b.WriteString("Live tasks are preserved; the daemon sweeps browsers of finished or missing tasks.\n")
	return b.String()
}
