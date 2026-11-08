package terminal

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/drn/argus/internal/testutil"
)

func testHeapBytes() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// feedLines writes n distinct dense lines so each scrolls into scrollback.
func feedLines(emu interface{ Write([]byte) (int, error) }, n, cols int) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%06d %s\r\n", i, strings.Repeat("x", cols-8))
	}
	_, _ = emu.Write([]byte(b.String()))
}

func TestReleaseScrollback_FreesRetainedLines(t *testing.T) {
	emu := newDrainedReplayEmulator(200, 40)
	base := testHeapBytes()
	feedLines(emu, 5000, 200)
	testutil.Equal(t, emu.ScrollbackLen() > 4000, true)
	full := testHeapBytes()
	if full-base < 50<<20 {
		t.Fatalf("expected >50MB retained, got %d", full-base)
	}

	// Documents the x/vt hazard: Clear alone keeps the peak.
	_, _ = SafeEmuWrite(emu, []byte("\x1bc"))
	emu.ClearScrollback()
	if after := testHeapBytes(); full-after > 5<<20 {
		t.Fatalf("x/vt ClearScrollback unexpectedly freed memory (%d); revisit ReleaseScrollback", full-after)
	}

	feedLines(emu, 5000, 200)
	ReleaseScrollback(emu)
	testutil.Equal(t, emu.ScrollbackLen(), 0)
	if after := testHeapBytes(); after > base+8<<20 {
		t.Fatalf("ReleaseScrollback left %d bytes retained over baseline", after-base)
	}
	runtime.KeepAlive(emu)
}

func TestTrimScrollbackToCells(t *testing.T) {
	emu := newDrainedReplayEmulator(100, 20)
	feedLines(emu, 1000, 100)
	before := emu.ScrollbackLen()
	t.Run("under budget is a no-op", func(t *testing.T) {
		testutil.Equal(t, TrimScrollbackToCells(emu, 1<<30, replayScrollbackSize), 0)
		testutil.Equal(t, emu.ScrollbackLen(), before)
	})
	t.Run("trims oldest first within budget", func(t *testing.T) {
		dropped := TrimScrollbackToCells(emu, 100*200, replayScrollbackSize)
		testutil.Equal(t, dropped > 0, true)
		testutil.Equal(t, emu.ScrollbackLen(), before-dropped)
		cells := 0
		for _, l := range emu.Scrollback().Lines() {
			cells += len(l)
		}
		testutil.Equal(t, cells <= 100*200, true)
		// Oldest-first: the NEWEST scrolled line survives, the oldest is gone.
		lineNo := func(i int) int {
			l := emu.Scrollback().Line(i)
			var sb strings.Builder
			for _, c := range l[:6] {
				sb.WriteString(c.Content)
			}
			n, err := strconv.Atoi(sb.String())
			testutil.NoError(t, err)
			return n
		}
		testutil.Equal(t, lineNo(emu.ScrollbackLen()-1) >= 970, true)
		testutil.Equal(t, lineNo(0) > 0, true)
	})
	t.Run("cap restored so later pushes still accumulate", func(t *testing.T) {
		n := emu.ScrollbackLen()
		feedLines(emu, 50, 100)
		testutil.Equal(t, emu.ScrollbackLen() > n, true)
	})
	t.Run("always keeps at least one line", func(t *testing.T) {
		TrimScrollbackToCells(emu, 1, replayScrollbackSize)
		testutil.Equal(t, emu.ScrollbackLen(), 1)
	})
}

func TestReleaseIdle(t *testing.T) {
	newPane := func() *TerminalPane {
		tp := NewTerminalPane()
		tp.replayEmu = newDrainedReplayEmulator(100, 20)
		tp.replayEmuCols, tp.replayEmuRows = 100, 20
		feedLines(tp.replayEmu, 200, 100)
		tp.replayEmuSpare = newDrainedReplayEmulator(100, 20)
		feedLines(tp.replayEmuSpare, 200, 100)
		tp.replayEmuSpareCols, tp.replayEmuSpareRows = 100, 20
		return tp
	}
	now := time.Now()
	t.Run("recently drawn pane untouched", func(t *testing.T) {
		tp := newPane()
		tp.lastDrawNanos.Store(now.UnixNano())
		testutil.Equal(t, tp.ReleaseIdle(now.Add(time.Minute), ReplayIdleRelease), false)
		testutil.Equal(t, tp.replayEmu != nil, true)
		testutil.Equal(t, tp.replayEmuSpare.ScrollbackLen() > 0, true)
	})
	t.Run("idle pane releases spare and published", func(t *testing.T) {
		tp := newPane()
		tp.lastDrawNanos.Store(now.UnixNano())
		testutil.Equal(t, tp.ReleaseIdle(now.Add(6*time.Minute), 5*time.Minute), true)
		testutil.Equal(t, tp.replayEmu == nil, true)
		testutil.Equal(t, tp.replayEmuSpare != nil && tp.replayEmuSpare.ScrollbackLen() == 0, true)
		testutil.Equal(t, tp.replayEmuParked != nil && tp.replayEmuParked.ScrollbackLen() == 0, true)
		testutil.Equal(t, tp.replayEmuMaxScroll, 0)
		testutil.Equal(t, tp.replayEmuFirstByte, int64(0))
		// Second call finds nothing left to do.
		testutil.Equal(t, tp.ReleaseIdle(now.Add(7*time.Minute), 5*time.Minute), false)
	})
	t.Run("rebuild in flight is skipped", func(t *testing.T) {
		tp := newPane()
		tp.lastDrawNanos.Store(now.UnixNano())
		tp.replayBuilding = true
		testutil.Equal(t, tp.ReleaseIdle(now.Add(time.Hour), 5*time.Minute), false)
		testutil.Equal(t, tp.replayEmu != nil, true)
	})
	t.Run("zero threshold disables", func(t *testing.T) {
		tp := newPane()
		testutil.Equal(t, tp.ReleaseIdle(now.Add(time.Hour), 0), false)
	})
}

func TestAcquireReusedSpareFreesScrollback(t *testing.T) {
	tp := NewTerminalPane()
	base := testHeapBytes()
	emu := newDrainedReplayEmulator(200, 40)
	feedLines(emu, 3000, 200)
	full := testHeapBytes()
	if full-base < 30<<20 {
		t.Fatalf("setup retained only %d bytes", full-base)
	}
	tp.replayEmuSpare, tp.replayEmuSpareCols, tp.replayEmuSpareRows = emu, 200, 40
	got := tp.acquireReplayEmulatorForRebuild(200, 40, nil)
	testutil.Equal(t, got == emu, true)
	testutil.Equal(t, got.ScrollbackLen(), 0)
	// A bare ClearScrollback would leave `full` retained; reuse must free it.
	if after := testHeapBytes(); after > base+8<<20 {
		t.Fatalf("reused spare still retains %d bytes over baseline", after-base)
	}
	runtime.KeepAlive(got)
}

func TestReplayRebuild_TrimsToCellBudgetAndBlocksExtend(t *testing.T) {
	old := replayScrollbackCellBudget
	replayScrollbackCellBudget = 5000
	defer func() { replayScrollbackCellBudget = old }()

	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "%06d %s\r\n", i, strings.Repeat("y", 90))
	}
	tp := NewTerminalPane()
	tp.asyncReplayRebuild("", 0, 20, 100, 20, []byte(b.String()), nil, 0, false, false, nil)

	tp.mu.Lock()
	emu, trimmed, maxScroll := tp.replayEmu, tp.replayEmuTrimmed, tp.replayEmuMaxScroll
	tp.mu.Unlock()
	testutil.Equal(t, emu != nil, true)
	testutil.Equal(t, trimmed, true)
	cells := 0
	for _, l := range emu.Scrollback().Lines() {
		cells += len(l)
	}
	testutil.Equal(t, cells <= 5000, true)
	// maxScroll reflects what is retained, not what was fed.
	testutil.Equal(t, maxScroll < 100, true)

	// A reset clears the trimmed latch so a fresh binding can extend again.
	tp.ResetVT()
	tp.mu.Lock()
	testutil.Equal(t, tp.replayEmuTrimmed, false)
	tp.mu.Unlock()
}

// TestReplayRebuild_HeapFollowsInputNotPeak is the offline creep check. Big
// rebuilds fill both slots; small rebuilds then reuse them. Retained heap must
// fall back toward the small size — with bare ClearScrollback it stays pinned
// at the peak (the ~1 MB/h creep) — and never exceeds the two slots.
func TestReplayRebuild_HeapFollowsInputNotPeak(t *testing.T) {
	if testing.Short() {
		t.Skip("slow heap measurement")
	}
	mk := func(n int) []byte {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "%06d %s\r\n", i, strings.Repeat("z", 180))
		}
		return []byte(b.String())
	}
	big, small := mk(700), mk(20)
	tp := NewTerminalPane()
	rebuild := func(ring []byte) {
		tp.asyncReplayRebuild("", 0, 40, 200, 40, ring, nil, 0, false, false, nil)
	}
	base := testHeapBytes()
	rebuild(big)
	rebuild(big)
	rebuild(big) // third rebuild reuses the retired slot
	peak := testHeapBytes() - base
	if peak < 30<<20 {
		t.Fatalf("setup retained only %d bytes", peak)
	}
	rebuild(small)
	rebuild(small)
	after := testHeapBytes() - base
	if after > peak/2 {
		t.Fatalf("heap stayed at %d of %d peak after small rebuilds", after, peak)
	}
	tp.mu.Lock()
	testutil.Equal(t, tp.replayEmuCreated, 2) // never more than the two slots
	testutil.Equal(t, tp.replayEmuDropped, 0)
	tp.mu.Unlock()
}

func TestDrawStampsLastDraw(t *testing.T) {
	tp := NewTerminalPane()
	tp.SetRect(0, 0, 80, 24)
	screen := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, screen.Init())
	screen.SetSize(80, 24)
	tp.lastDrawNanos.Store(1)
	tp.Draw(screen)
	testutil.Equal(t, tp.lastDrawNanos.Load() > 1, true)
	// A just-drawn pane is never idle-released.
	testutil.Equal(t, tp.ReleaseIdle(time.Now(), ReplayIdleRelease), false)
}

func TestReplayExtendWanted(t *testing.T) {
	mk := func(trimmed bool) *TerminalPane {
		tp := NewTerminalPane()
		tp.replayEmu = newDrainedReplayEmulator(100, 20)
		tp.replayEmuCols, tp.replayEmuRows = 100, 20
		tp.replayEmuFirstByte = 4096
		tp.replayEmuMaxScroll = 50
		tp.replayEmuTrimmed = trimmed
		return tp
	}
	cases := []struct {
		name   string
		tp     *TerminalPane
		alive  bool
		cols   int
		offset int
		want   bool
	}{
		{"scrolled past top extends", mk(false), true, 100, 80, true},
		{"within window does not", mk(false), true, 100, 50, false},
		{"trimmed build never extends", mk(true), true, 100, 80, false},
		{"dead session never extends", mk(false), false, 100, 80, false},
		{"dimension change wants fresh read", mk(false), true, 120, 80, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testutil.Equal(t, c.tp.replayExtendWantedLocked(c.alive, c.cols, 20, c.offset), c.want)
		})
	}
	t.Run("no log-backed first byte", func(t *testing.T) {
		tp := mk(false)
		tp.replayEmuFirstByte = 0
		testutil.Equal(t, tp.replayExtendWantedLocked(true, 100, 20, 80), false)
	})
}
