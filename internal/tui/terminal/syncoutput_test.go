package terminal

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

const (
	syncH = "\x1b[?2026h"
	syncL = "\x1b[?2026l"
)

func TestSyncHoldLen(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"plain", "hello", 0},
		{"closed block", "a" + syncH + "xyz" + syncL + "b", 0},
		{"open block holds from its ESC", "ab" + syncH + "xyz", len(syncH + "xyz")},
		{"block opening at very end", "ab" + syncH, len(syncH)},
		{"duplicate h h keeps the FIRST start", "a" + syncH + "x" + syncH + "y", len(syncH + "x" + syncH + "y")},
		{"h h l closes (modes do not nest)", "a" + syncH + syncH + "x" + syncL + "z", 0},
		{"h l h reopens at the second h", syncH + "x" + syncL + "q" + syncH + "w", len(syncH + "w")},
		{"stray l is ignored", "a" + syncL + "b", 0},
		{"partial prefix ESC", "abc\x1b", 1},
		{"partial prefix ESC[?20", "abc\x1b[?20", 5},
		{"partial prefix without final byte", "abc\x1b[?2026", 7},
		{"non-matching CSI is not held", "abc\x1b[?25l", 0},
		{"different final after 2026 is not held", "abc\x1b[?2026$", 0},
		{"other private mode 20260 is not a block", "abc\x1b[?20260h", 0},
		{"prefix then ESC restarts the match", "\x1b[?2\x1b[?2026h" + "x", len(syncH + "x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, syncHoldLen([]byte(tt.in)), tt.want)
		})
	}
}

// A block split at every possible byte boundary must be held until it closes:
// the safe prefix is always at or before the block start, and the whole stream
// is eventually released intact.
func TestSyncHoldLen_EverySplitPoint(t *testing.T) {
	stream := []byte("pre" + syncH + "frame-body" + syncL + "post")
	blockStart := len("pre")
	blockEnd := blockStart + len(syncH+"frame-body"+syncL)
	for cut := 1; cut < len(stream); cut++ {
		hold := syncHoldLen(stream[:cut])
		safe := cut - hold
		if cut > blockStart && cut < blockEnd && safe > blockStart {
			t.Fatalf("cut=%d: safe prefix %d runs into the open block starting at %d", cut, safe, blockStart)
		}
		if cut >= blockEnd && hold != 0 {
			t.Fatalf("cut=%d: closed block still held %d bytes", cut, hold)
		}
	}
}

func TestSyncTracker(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := &SyncTracker{now: func() time.Time { return now }}

	testutil.Equal(t, tr.Observe([]byte("plain")), true)
	testutil.Equal(t, tr.Observe([]byte("x"+syncH)), true)       // opening write wakes
	testutil.Equal(t, tr.Observe([]byte("inside")), false)       // wholly inside: suppressed
	testutil.Equal(t, tr.Observe([]byte("more\x1b[?20")), false) // split close prefix, still inside
	testutil.Equal(t, tr.Observe([]byte("26l tail")), true)      // closing write (split sequence) wakes
	testutil.Equal(t, tr.Observe([]byte("after")), true)

	// Stuck open: a lost ?2026l must not suppress wakes forever.
	testutil.Equal(t, tr.Observe([]byte(syncH)), true)
	testutil.Equal(t, tr.Observe([]byte("inside")), false)
	now = now.Add(SyncOutputTimeout + time.Millisecond)
	testutil.Equal(t, tr.Observe([]byte("still no close")), true)
}

// livePane binds a pane to a mock live session already holding "base" so the
// first Draw takes the ordinary fresh-bind history path.
func livePane(t *testing.T, base string) (*TerminalPane, *mockAdapter, tcell.SimulationScreen) {
	t.Helper()
	sess := &mockAdapter{alive: true, ptyCols: 80, ptyRows: 24}
	sess.append(base)
	tp := NewTerminalPane()
	tp.SetSession(sess)
	tp.SetRect(0, 0, 80, 24)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	drawFrame(tp, screen)
	return tp, sess, screen
}

func (m *mockAdapter) append(s string) {
	m.output = append(m.output, s...)
	m.totalWritten = uint64(len(m.output))
}

func screenRow(screen tcell.SimulationScreen, y int) string {
	screen.Show()
	cells, w, _ := screen.GetContents()
	var sb strings.Builder
	for x := 0; x < w; x++ {
		c := cells[(y+1)*w+x] // row 0 is the pane border
		if len(c.Runes) > 0 {
			sb.WriteRune(c.Runes[0])
		}
	}
	return strings.Trim(sb.String(), " │")
}

func TestSyncGate_HoldsOpenBlockThenPaintsOnClose(t *testing.T) {
	tp, sess, screen := livePane(t, "\x1b[Hold frame")
	testutil.Equal(t, screenRow(screen, 0), "old frame")

	// Frame begins; only half of it has arrived.
	sess.append(syncH + "\x1b[Hnew fr")
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "old frame") // last complete frame stays
	if tp.emuFedTotal >= sess.totalWritten {
		t.Fatalf("emuFedTotal=%d should stop short of total=%d while the block is open", tp.emuFedTotal, sess.totalWritten)
	}

	// Block closes: the whole frame lands at once.
	sess.append("ame" + syncL)
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "new frame")
	testutil.Equal(t, tp.emuFedTotal, sess.totalWritten)
}

func TestSyncGate_SplitOpenSequenceIsNotParsedEarly(t *testing.T) {
	tp, sess, screen := livePane(t, "\x1b[Hold")
	// The opener itself is split across two reads.
	sess.append("\x1b[?20")
	drawFrame(tp, screen)
	testutil.Equal(t, tp.emuFedTotal, sess.totalWritten-5)
	sess.append("26h\x1b[Hnew")
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "old")
	sess.append(syncL)
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "new")
}

func TestSyncGate_TimeoutFlushesAndWakes(t *testing.T) {
	tp, sess, screen := livePane(t, "\x1b[Hold")
	clock := time.Unix(5000, 0)
	tp.syncNow = func() time.Time { return clock }
	var wakes []time.Duration
	tp.OnSyncHold = func(d time.Duration) { wakes = append(wakes, d) }

	sess.append(syncH + "\x1b[Hstuck")
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "old")
	drawFrame(tp, screen) // a second draw inside the window must not re-arm the wake
	testutil.DeepEqual(t, wakes, []time.Duration{SyncOutputTimeout})

	clock = clock.Add(SyncOutputTimeout - time.Millisecond)
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "old")
	testutil.Equal(t, tp.syncExpired, uint64(0))

	clock = clock.Add(2 * time.Millisecond)
	drawFrame(tp, screen)
	testutil.Equal(t, screenRow(screen, 0), "stuck") // never freezes
	testutil.Equal(t, tp.emuFedTotal, sess.totalWritten)
	testutil.Equal(t, tp.syncExpired, uint64(1))

	// The late close arrives afterwards: harmless, nothing is held again.
	sess.append(syncL + "!")
	drawFrame(tp, screen)
	testutil.Equal(t, tp.emuFedTotal, sess.totalWritten)
}

func TestSyncGate_RunawayHoldIsCapped(t *testing.T) {
	tp, sess, screen := livePane(t, "\x1b[Hold")
	sess.append(syncH + strings.Repeat("x", syncOutputMaxHold))
	drawFrame(tp, screen)
	testutil.Equal(t, tp.emuFedTotal, sess.totalWritten) // over the byte cap: flushed
}

func TestSyncGate_ResetOnEveryRebind(t *testing.T) {
	hold := func(t *testing.T) (*TerminalPane, *mockAdapter, tcell.SimulationScreen) {
		tp, sess, screen := livePane(t, "\x1b[Hold")
		sess.append(syncH + "half")
		drawFrame(tp, screen)
		if tp.syncHeldSince.IsZero() {
			t.Fatal("precondition: pane should be holding an open block")
		}
		return tp, sess, screen
	}
	t.Run("ResetVT", func(t *testing.T) {
		tp, _, _ := hold(t)
		tp.ResetVT()
		if !tp.syncHeldSince.IsZero() || tp.syncHeldAt != 0 {
			t.Error("ResetVT left a stale open block")
		}
	})
	t.Run("SetSession to a new session", func(t *testing.T) {
		tp, _, screen := hold(t)
		fresh := &mockAdapter{alive: true, ptyCols: 80, ptyRows: 24}
		fresh.append("\x1b[Hnext")
		tp.SetSession(fresh)
		if !tp.syncHeldSince.IsZero() {
			t.Error("rebind left a stale open block")
		}
		drawFrame(tp, screen)
		testutil.Equal(t, screenRow(screen, 0), "next")
		testutil.Equal(t, tp.emuFedTotal, fresh.totalWritten)
	})
	t.Run("in-place emulator reset", func(t *testing.T) {
		tp, _, _ := hold(t)
		tp.resetLiveEmulatorInPlace()
		if !tp.syncHeldSince.IsZero() {
			t.Error("in-place reset left a stale open block")
		}
	})
}

// Real bytes: a 30KB window of a real Claude Code session log (246 balanced
// ?2026h…?2026l frames of spinner/status redraws; see testdata). Replays it in
// arbitrary read-sized chunks and asserts the pane NEVER parses (hence never
// paints) a position strictly inside a block, and that nothing is lost.
func TestSyncGate_RealClaudeFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/claude_sync2026_window.bin")
	if err != nil {
		t.Fatal(err)
	}
	spans := syncSpans(fixture)
	testutil.Equal(t, len(spans), 246)
	inside := func(off uint64) bool {
		for _, s := range spans {
			if off > uint64(s[0]) && off < uint64(s[1]) { //nolint:gosec // test offsets
				return true
			}
		}
		return false
	}

	chunkOf := func(i int) int { return 150 + (i*7919)%700 } // deterministic, 150–849B reads
	run := func(gated bool) (tornPaints, paints int, finalRow string) {
		sess := &mockAdapter{alive: true, ptyCols: 120, ptyRows: 40}
		sess.append("\x1b[H")
		tp := NewTerminalPane()
		tp.SetSession(sess)
		tp.SetRect(0, 0, 120, 40)
		screen := tcell.NewSimulationScreen("UTF-8")
		if err := screen.Init(); err != nil {
			t.Fatal(err)
		}
		screen.SetSize(120, 40)
		drawFrame(tp, screen)
		base := sess.totalWritten
		tp.syncNow = func() time.Time { return time.Unix(1, 0) } // frozen: timeout can't mask a bug
		for off, i := 0, 0; off < len(fixture); i++ {
			n := min(chunkOf(i), len(fixture)-off)
			sess.append(string(fixture[off : off+n]))
			off += n
			if !gated {
				// "before": the pre-change behaviour, everything fed at once.
				tp.syncDisabled = true
			}
			drawFrame(tp, screen)
			paints++
			if inside(tp.emuFedTotal - base) {
				tornPaints++
			}
		}
		drawFrame(tp, screen)
		testutil.Equal(t, tp.emuFedTotal, sess.totalWritten)
		var rows []string
		for y := 0; y < 38; y++ {
			rows = append(rows, screenRow(screen, y))
		}
		return tornPaints, paints, strings.Join(rows, "\n")
	}

	tornAfter, paints, rowsAfter := run(true)
	tornBefore, _, rowsBefore := run(false)
	t.Logf("paints=%d torn_before=%d torn_after=%d", paints, tornBefore, tornAfter)
	testutil.Equal(t, tornAfter, 0)
	if tornBefore == 0 {
		t.Fatal("fixture/chunking never lands a paint inside a block without the gate; the test proves nothing")
	}
	if !bytes.Equal([]byte(rowsAfter), []byte(rowsBefore)) {
		t.Errorf("final screen differs with the gate:\n--- gated\n%s\n--- ungated\n%s", rowsAfter, rowsBefore)
	}
}

// syncSpans returns [start,end) byte offsets of every ?2026h…?2026l block.
func syncSpans(b []byte) [][2]int {
	var d syncDFA
	var spans [][2]int
	start := -1
	for i, c := range b {
		switch d.step(c) {
		case 1:
			start = i - len(syncSeq)
		case -1:
			spans = append(spans, [2]int{start, i + 1})
		}
	}
	return spans
}

// drawFrame mimics tview's per-frame Clear before the widget draws; without it
// cells the pane no longer paints would keep their previous contents.
func drawFrame(tp *TerminalPane, screen tcell.SimulationScreen) {
	screen.Clear()
	tp.Draw(screen)
}
