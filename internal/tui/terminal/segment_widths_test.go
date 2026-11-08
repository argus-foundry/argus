package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/testutil"
)

// segLog builds a log whose first segment was authored at 20 cols and whose
// second at 40 cols (the PTY was widened between them). Segment 1 draws a UI
// that wraps to TWO rows at 20 cols, then redraws it with a relative
// cursor-up of 2 — correct at 20 cols, but at 40 cols the UI is one row, so
// the same bytes walk up onto HEADER and erase it.
func segLog() (log string, widenAt int64) {
	seg1 := "HEADER\r\n" + strings.Repeat("A", 30) + "\r\n" + "\x1b[2A\x1b[J" + strings.Repeat("B", 30) + "\r\n"
	seg2 := "TAIL " + strings.Repeat("C", 30) + "\r\n"
	return seg1 + seg2, int64(len(seg1))
}

func dumpRows(t *testing.T, emuCell func(x, y int) string, cols, rows int) []string {
	t.Helper()
	var out []string
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < cols; x++ {
			c := emuCell(x, y)
			if c == "" {
				c = " "
			}
			b.WriteString(c)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func feedSegLog(t *testing.T, marks []agent.SizeMark) []string {
	t.Helper()
	log, _ := segLog()
	emu := newDrainedReplayEmulator(40, 8)
	end, _ := feedReplayChunksSized(emu, strings.NewReader(log), 0, int64(len(log)), 0, 40, 8, 0, false, marks)
	testutil.Equal(t, end, int64(len(log)))
	// The emulator must come back at the dims the caller built it for.
	testutil.Equal(t, emu.Width(), 40)
	testutil.Equal(t, emu.Height(), 8)
	return dumpRows(t, func(x, y int) string {
		if c := emu.CellAt(x, y); c != nil {
			return c.Content
		}
		return ""
	}, 40, 8)
}

func TestFeedReplayChunksSized_SegmentsKeepRelativeRedrawsIntact(t *testing.T) {
	_, widenAt := segLog()
	rows := feedSegLog(t, []agent.SizeMark{{Off: 0, Cols: 20, Rows: 8}, {Off: widenAt, Cols: 40, Rows: 8}})
	testutil.Equal(t, rows[0], "HEADER")
	testutil.Equal(t, rows[1], strings.Repeat("B", 20))
	testutil.Equal(t, rows[2], strings.Repeat("B", 10))
	testutil.Contains(t, strings.Join(rows, "\n"), "TAIL "+strings.Repeat("C", 30))
}

// Control: the same bytes with no index (one width for the whole log) erase
// HEADER — the failure the markers exist to prevent.
func TestFeedReplayChunksSized_NoMarksLosesHeader(t *testing.T) {
	rows := feedSegLog(t, nil)
	if strings.Contains(strings.Join(rows, "\n"), "HEADER") {
		t.Fatalf("control: expected HEADER erased without width marks, got %q", rows)
	}
}

// A window that starts mid-log begins at the width in force at its start.
func TestFeedReplayChunksSized_WindowStartsAtMarkedWidth(t *testing.T) {
	log, widenAt := segLog()
	emu := newDrainedReplayEmulator(40, 8)
	marks := []agent.SizeMark{{Off: 0, Cols: 20, Rows: 8}, {Off: widenAt, Cols: 40, Rows: 8}}
	_, _ = feedReplayChunksSized(emu, strings.NewReader(log), widenAt, int64(len(log)), 0, 40, 8, 0, true, marks)
	rows := dumpRows(t, func(x, y int) string {
		if c := emu.CellAt(x, y); c != nil {
			return c.Content
		}
		return ""
	}, 40, 8)
	testutil.Contains(t, strings.Join(rows, "\n"), "TAIL "+strings.Repeat("C", 30))
	testutil.Equal(t, emu.Width(), 40)
}

// Draw-level: an alive session whose PTY is now 40 wide (initial 20) replays
// through the real scroll path using the on-disk index.
func TestInterimView_ScrollReplayHonorsSizesIndex(t *testing.T) {
	log, widenAt := segLog()
	run := func(withIndex bool) string {
		setupTaskLog(t, "segs", log)
		if withIndex {
			agent.ResetSessionSizes("segs", 20, 8)
			agent.AppendSessionSizeMark("segs", widenAt, 40, 8)
		}
		sim := newSim(t, 42, 12)
		tp := NewTerminalPane()
		tp.SetRect(0, 0, 42, 12)
		sess := &initialSizeAdapter{
			mockAdapter: &mockAdapter{alive: true, ptyCols: 40, ptyRows: 10, totalWritten: 6, output: []byte("tail\r\n")},
			initCols:    20, initRows: 8,
		}
		tp.SetSession(sess)
		tp.SetTaskID("segs")
		tp.scrollOffset = 1
		tp.Draw(sim)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			tp.mu.Lock()
			building := tp.replayBuilding
			tp.mu.Unlock()
			if !building {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		tp.Draw(sim)
		return readScreen(sim)
	}
	// Segment 1's UI was authored at 20 cols, so with the index it wraps to
	// 20+10 B's; emulated at 40 for the whole log (no index) the same bytes
	// land as one 30-wide row (and walk up over HEADER).
	wrapped := strings.Repeat("B", 30)
	if with := run(true); strings.Contains(with, wrapped) || !strings.Contains(with, strings.Repeat("B", 20)) {
		t.Fatalf("with index: segment 1 should wrap at its authored 20 cols:\n%s", with)
	}
	if without := run(false); !strings.Contains(without, wrapped) {
		t.Fatalf("control: without the index the log is emulated at one width:\n%s", without)
	}
}

// A DEAD session's replay stops at its final alt-screen exit (#1174's whole-log
// bound) AND honors the width index: the segments before the bound are
// emulated at their authored widths, and nothing after the bound is fed.
func TestInterimView_DeadSessionBoundComposesWithSizesIndex(t *testing.T) {
	log, widenAt := segLog()
	log += "\x1b[?1049l" + "RESUME-HINT-AFTER-EXIT"
	setupTaskLog(t, "deadsegs", log)
	agent.SaveSessionSize("deadsegs", 40, 8) // dead session: width comes from the .size sidecar
	agent.ResetSessionSizes("deadsegs", 20, 8)
	agent.AppendSessionSizeMark("deadsegs", widenAt, 40, 8)

	sim := newSim(t, 42, 12)
	tp := NewTerminalPane()
	tp.SetRect(0, 0, 42, 12)
	tp.SetTaskID("deadsegs") // no session: the dead-session replay path
	tp.Draw(sim)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		tp.mu.Lock()
		building := tp.replayBuilding
		tp.mu.Unlock()
		if !building {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	tp.Draw(sim)
	screen := readScreen(sim)

	if strings.Contains(screen, "RESUME-HINT") {
		t.Fatalf("dead replay must stop at the final alt-screen exit:\n%s", screen)
	}
	if strings.Contains(screen, strings.Repeat("B", 30)) || !strings.Contains(screen, strings.Repeat("B", 20)) {
		t.Fatalf("segment 1 should replay at its authored 20 cols before the bound:\n%s", screen)
	}
	testutil.Contains(t, screen, "TAIL "+strings.Repeat("C", 30))
}
