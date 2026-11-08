package terminal

import (
	"time"

	xvt "github.com/charmbracelet/x/vt"
)

// Memory-bound constants for pane emulators. A uv.Cell is 112 bytes (measured
// on real session logs; see gotchas/pty-terminal.md), so a cell budget is a
// byte budget: replayScrollbackCellBudget ≈ 112 MB per replay emulator.
// A var so tests can shrink it; production never mutates it.
var replayScrollbackCellBudget = 1_000_000

// ReplayIdleRelease is how long a pane may go undrawn before ReleaseIdle
// shrinks its replay emulators.
const ReplayIdleRelease = 5 * time.Minute

// ReleaseScrollback empties emu's scrollback AND drops the references x/vt
// keeps to the old lines. x/vt's Scrollback.Clear is `lines = lines[:0]`,
// which leaves every old line reachable through the backing array — so a bare
// ClearScrollback frees nothing and a RIS-reused emulator holds its peak
// forever. Zeroing the elements first makes them collectible. The emulator
// itself is kept (never Closed: its drain goroutine keeps it reachable and
// Close races that Read). emu must not be concurrently painted.
func ReleaseScrollback(emu *xvt.SafeEmulator) {
	if sb := emu.Scrollback(); sb != nil {
		clear(sb.Lines())
	}
	emu.ClearScrollback()
}

// TrimScrollbackToCells drops the OLDEST scrollback lines until the total
// cell count is within budget (always keeping at least the newest line), and
// returns how many lines were dropped. Dropped lines are zeroed so they are
// collectible. maxLines is the emulator's normal line cap, restored after the
// trim (SetMaxLines is the only trimming API x/vt exposes and it also lowers
// the cap). emu must not be concurrently painted.
func TrimScrollbackToCells(emu *xvt.SafeEmulator, budget, maxLines int) int {
	sb := emu.Scrollback()
	if sb == nil {
		return 0
	}
	lines := sb.Lines()
	cells, keep := 0, 0
	for i := len(lines) - 1; i >= 0; i-- {
		if keep > 0 && cells+len(lines[i]) > budget {
			break
		}
		cells += len(lines[i])
		keep++
	}
	drop := len(lines) - keep
	if drop <= 0 {
		return 0
	}
	clear(lines[:drop])
	emu.SetScrollbackSize(keep)
	emu.SetScrollbackSize(maxLines)
	return drop
}
