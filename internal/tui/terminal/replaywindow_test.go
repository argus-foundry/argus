package terminal

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/testutil"
)

func TestCheckpointMaps(t *testing.T) {
	cps := []replayCheckpoint{{100, 0}, {200, 10}, {300, 25}, {400, 40}}
	t.Run("byteRangeForLine", func(t *testing.T) {
		lo, hi := byteRangeForLine(cps, 12, 450)
		testutil.Equal(t, lo, int64(200))
		testutil.Equal(t, hi, int64(300))
		lo, hi = byteRangeForLine(cps, 99, 450) // past last checkpoint → screen region
		testutil.Equal(t, lo, int64(400))
		testutil.Equal(t, hi, int64(450))
	})
	t.Run("lineRangeForBytes", func(t *testing.T) {
		lo, hi := lineRangeForBytes(cps, 210, 290, 50)
		testutil.Equal(t, lo, 10)
		testutil.Equal(t, hi, 25)
		lo, hi = lineRangeForBytes(cps, 410, 500, 50)
		testutil.Equal(t, lo, 40)
		testutil.Equal(t, hi, 50) // past the end → total
	})
	t.Run("checkpointsAfterTrim rebases and picks a conservative start", func(t *testing.T) {
		out, start := checkpointsAfterTrim(cps, 12)
		testutil.Equal(t, start, int64(200)) // first retained line sits in chunk (200,300]
		testutil.Equal(t, fmt.Sprint(out), fmt.Sprint([]replayCheckpoint{{200, 0}, {300, 13}, {400, 28}}))
		same, s0 := checkpointsAfterTrim(cps, 0)
		testutil.Equal(t, fmt.Sprint(same), fmt.Sprint(cps))
		testutil.Equal(t, s0, int64(100))
	})
}

// A chunked feed must produce byte-for-byte the same screen+scrollback as one
// Write, including when chunk boundaries fall inside escape sequences.
func TestFeedReplayChunks_MatchesOneShot(t *testing.T) {
	old := replayFeedChunk
	replayFeedChunk = 37 // tiny: boundaries land inside CSI/OSC sequences
	defer func() { replayFeedChunk = old }()

	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "\x1b[3%dm%04d\x1b[0m \x1b]0;title %d\x07text\x1b[1;5r line %d\r\n", i%7, i, i, i)
	}
	raw := []byte(b.String())

	one := newDrainedReplayEmulator(60, 10)
	_, _ = SafeEmuWrite(one, FilterOSC(ClampScrollRegion(AlignToEscBoundary(raw), 60, 10)))

	chunked := newDrainedReplayEmulator(60, 10)
	end, cps := feedReplayChunks(chunked, strings.NewReader(string(raw)), 0, int64(len(raw)), 1000, 60, 10, 0, true)
	testutil.Equal(t, end, int64(1000+len(raw)))
	testutil.Equal(t, cps[0], replayCheckpoint{off: 1000, lines: 0})
	testutil.Equal(t, chunked.ScrollbackLen(), one.ScrollbackLen())
	for i := 0; i < one.ScrollbackLen(); i++ {
		testutil.Equal(t, unifiedRowText(chunked, chunked.ScrollbackLen(), i, 60), unifiedRowText(one, one.ScrollbackLen(), i, 60))
	}
	last := cps[len(cps)-1]
	testutil.Equal(t, last.off, end)
	testutil.Equal(t, last.lines, chunked.ScrollbackLen())
}

func TestFeedReplayChunks_StopsAtBudget(t *testing.T) {
	old := replayFeedChunk
	replayFeedChunk = 2048
	defer func() { replayFeedChunk = old }()
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "%06d %s\r\n", i, strings.Repeat("q", 60))
	}
	raw := b.String()
	emu := newDrainedReplayEmulator(80, 10)
	end, _ := feedReplayChunks(emu, strings.NewReader(raw), 0, int64(len(raw)), 0, 80, 10, 5000, false)
	testutil.Equal(t, end < int64(len(raw)), true)
	cells := scrollbackCells(emu)
	testutil.Equal(t, cells >= 5000, true)
	testutil.Equal(t, cells < 5000+2048, true) // overshoot bounded by ~one chunk
}

var visNum = regexp.MustCompile(`L(\d{6})`)

// visibleLineNumbers returns the L###### markers painted on screen, top to
// bottom.
func visibleLineNumbers(screen tcell.SimulationScreen) []int {
	w, h := screen.Size()
	cells, _, _ := screen.GetContents()
	var nums []int
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				sb.WriteRune(c.Runes[0])
			} else {
				sb.WriteByte(' ')
			}
		}
		if m := visNum.FindStringSubmatch(sb.String()); m != nil {
			n, _ := strconv.Atoi(m[1])
			nums = append(nums, n)
		}
	}
	return nums
}

// drawSettled draws until any async replay rebuild has finished and its result
// has been consumed and painted.
func drawSettled(t *testing.T, tp *TerminalPane, screen tcell.SimulationScreen) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		tp.Draw(screen)
		tp.mu.Lock()
		busy := tp.replayBuilding || tp.replayRebuildPending
		tp.mu.Unlock()
		if !busy {
			screen.Show()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("replay rebuild did not settle")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestReplayWindow_FullTraversal walks a log ~7x the cell budget from the live
// bottom to the very first line and back, through the real Draw path. It
// asserts content continuity at every step (including across window seams),
// that the cursor stays on the same content line when a window slides, that
// the very first and last lines are reachable, and that memory plateaus.
func TestReplayWindow_FullTraversal(t *testing.T) {
	oldBudget, oldChunk := replayScrollbackCellBudget, replayFeedChunk
	replayScrollbackCellBudget, replayFeedChunk = 30_000, 4096
	defer func() { replayScrollbackCellBudget, replayFeedChunk = oldBudget, oldChunk }()
	t.Setenv("HOME", t.TempDir())

	const total, cols, rows = 3000, 80, 24
	const taskID = "traversal-task"
	testutil.NoError(t, os.MkdirAll(agent.SessionsDir(), 0o755))
	var b strings.Builder
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "L%06d %s\r\n", i, strings.Repeat("w", 60))
	}
	testutil.NoError(t, os.WriteFile(agent.SessionLogPath(taskID), []byte(b.String()), 0o600))

	tp := NewTerminalPane()
	tp.SetRect(0, 0, cols+2, rows+2)
	screen := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, screen.Init())
	screen.SetSize(cols+2, rows+2)
	sess := &mockAdapter{alive: true, ptyCols: cols, ptyRows: rows}
	tp.SetSession(sess)
	tp.SetTaskID(taskID)
	tp.Draw(screen)

	const step = 7
	checkWindow := func(label string) []int {
		nums := visibleLineNumbers(screen)
		if len(nums) < 10 {
			t.Fatalf("%s: only %d numbered rows visible", label, len(nums))
		}
		for i := 1; i < len(nums); i++ {
			if nums[i] != nums[i-1]+1 {
				t.Fatalf("%s: visible lines not contiguous: %v", label, nums)
			}
		}
		tp.mu.Lock()
		if cells := scrollbackCells(tp.replayEmu); cells > replayScrollbackCellBudget+replayFeedChunk {
			t.Fatalf("%s: window holds %d cells, budget %d", label, cells, replayScrollbackCellBudget)
		}
		tp.mu.Unlock()
		return nums
	}

	tp.ScrollUp(step)
	drawSettled(t, tp, screen)
	top := checkWindow("first scroll")[0]
	heapAfterFirst := testHeapBytes()
	slides := 0
	prevEnd := int64(-1)
	for top > 0 {
		tp.ScrollUp(step)
		drawSettled(t, tp, screen)
		nums := checkWindow(fmt.Sprintf("up@%d", top))
		want := top - step
		if want < 0 {
			want = 0
		}
		if nums[0] != want {
			t.Fatalf("scroll up from top=%d: got top=%d want %d (cursor line moved)", top, nums[0], want)
		}
		top = nums[0]
		tp.mu.Lock()
		if tp.replayEmuEnd != prevEnd {
			slides++
			prevEnd = tp.replayEmuEnd
		}
		tp.mu.Unlock()
	}
	testutil.Equal(t, top, 0) // very first line reached
	if slides < 4 {
		t.Fatalf("expected the window to slide several times, saw %d window states", slides)
	}
	if grown := int64(testHeapBytes()) - int64(heapAfterFirst); grown > 24<<20 {
		t.Fatalf("heap grew %d bytes during the upward traversal", grown)
	}

	// Back down to the live bottom.
	fwd := 0
	for guard := 0; guard < 2000; guard++ {
		before := visibleLineNumbers(screen)[0]
		tp.ScrollDown(step)
		if tp.ScrollOffset() == 0 {
			break
		}
		drawSettled(t, tp, screen)
		tp.mu.Lock()
		if tp.replayEmuEnd != prevEnd {
			fwd++
			prevEnd = tp.replayEmuEnd
		}
		tp.mu.Unlock()
		nums := checkWindow(fmt.Sprintf("down@%d", before))
		if nums[0] != before+step && nums[0] <= before {
			t.Fatalf("scroll down from top=%d: got top=%d (went backwards)", before, nums[0])
		}
	}
	testutil.Equal(t, tp.ScrollOffset(), 0)
	if fwd < 3 {
		t.Fatalf("expected the window to slide forward several times, saw %d window states", fwd)
	}
	// The last frames before live must have shown the real tail of the log.
	tp.ScrollUp(1)
	drawSettled(t, tp, screen)
	nums := checkWindow("tail")
	testutil.Equal(t, nums[len(nums)-1] >= total-3, true)
	if grown := int64(testHeapBytes()) - int64(heapAfterFirst); grown > 24<<20 {
		t.Fatalf("heap grew %d bytes over the full round trip", grown)
	}
}

func TestAnchorEdgeCases(t *testing.T) {
	emu := newDrainedReplayEmulator(80, 10)
	feedLines(emu, 200, 80)
	cps := []replayCheckpoint{{0, 0}, {1000, emu.ScrollbackLen()}}

	t.Run("no checkpoints cannot anchor", func(t *testing.T) {
		_, ok := captureAnchor(emu, nil, 1000, 80, 10, 5, 10, false)
		testutil.Equal(t, ok, false)
	})
	t.Run("blank content carries no signature", func(t *testing.T) {
		blank := newDrainedReplayEmulator(80, 10)
		a, ok := captureAnchor(blank, []replayCheckpoint{{0, 0}}, 10, 80, 10, 0, 10, false)
		testutil.Equal(t, ok, true)
		testutil.Equal(t, len(a.sig), 0)
	})
	t.Run("signature is taken below the unreliable window-top rows", func(t *testing.T) {
		a, ok := captureAnchor(emu, cps, 1000, 80, 10, emu.ScrollbackLen()+10, 10, false)
		testutil.Equal(t, ok, true)
		testutil.Equal(t, a.sigSkip, replayAnchorSkipTop)
		testutil.Equal(t, len(a.sig) >= 3, true)
	})
	t.Run("no signature falls back to the checkpoint estimate", func(t *testing.T) {
		line, matched := locateAnchor(emu, cps, viewAnchor{offLo: 0, offHi: 1000}, 80, 10, false)
		testutil.Equal(t, matched, false)
		testutil.Equal(t, line > 0, true)
	})
	t.Run("unmatched signature falls back to the estimate", func(t *testing.T) {
		a := viewAnchor{offLo: 0, offHi: 1000, sig: []string{"does not exist anywhere", "nor this", "nor this"}}
		_, matched := locateAnchor(emu, cps, a, 80, 10, false)
		testutil.Equal(t, matched, false)
	})
	t.Run("matched signature is located exactly", func(t *testing.T) {
		sbLen := emu.ScrollbackLen()
		a := viewAnchor{offLo: 0, offHi: 1000, sig: []string{unifiedRowText(emu, sbLen, 50, 80), unifiedRowText(emu, sbLen, 51, 80), unifiedRowText(emu, sbLen, 52, 80)}, sigSkip: 2}
		line, matched := locateAnchor(emu, cps, a, 80, 10, false)
		testutil.Equal(t, matched, true)
		testutil.Equal(t, line, 48)
	})
}

func TestPlanWindow_NilWithoutLogBackedWindow(t *testing.T) {
	tp := NewTerminalPane()
	testutil.Equal(t, tp.planWindowLocked(kickBack, 80, 24, 100, 24) == nil, true)
	tp.replayEmu = newDrainedReplayEmulator(80, 24)
	testutil.Equal(t, tp.planWindowLocked(kickForward, 80, 24, 100, 24) == nil, true) // no checkpoints
}

// An unreadable log must leave the published window alone, set the no-data
// cooldown, park the acquired emulator and release the single-flight flag.
func TestWindowRebuild_UnreadableLogKeepsWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tp := NewTerminalPane()
	published := newDrainedReplayEmulator(80, 24)
	tp.replayEmu, tp.replayEmuCols, tp.replayEmuRows = published, 80, 24
	tp.replayBuilding = true
	req := &replayWindowReq{start: 10, viewport: 24}
	tp.asyncReplayRebuildAt(tp.replayGen, "no-such-task", 5, 24, 80, 24, nil, nil, 0, false, false, req, nil)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	testutil.Equal(t, tp.replayEmu == published, true)
	testutil.Equal(t, tp.replayBuilding, false)
	testutil.Equal(t, tp.replayRebuildPending, false)
	testutil.Equal(t, tp.replayNoDataUntil.After(time.Now()), true)
	testutil.Equal(t, tp.replayEmuSpare != nil, true) // acquired emulator parked, not leaked
}
