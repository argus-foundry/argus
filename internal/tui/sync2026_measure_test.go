package tui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/terminal"
	"github.com/gdamore/tcell/v2"
)

// plainWriter is the pre-change repaintWriter: every write wakes the pump.
type plainWriter struct{ p *repaintPump }

func (w plainWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		w.p.wake()
	}
	return len(b), nil
}

type syncRig struct {
	ui       sync.Mutex // stands in for the tview goroutine
	pane     *terminal.TerminalPane
	sim      tcell.SimulationScreen
	sess     *repaintSession
	pump     *repaintPump
	tr       *typingTracker
	spans    [][2]uint64 // block [start,end) in session offsets
	frames   int
	painting int // frames that advanced the fed cursor
	torn     int // frames whose fed cursor sits strictly inside a block
	lastFed  uint64
	fedAt    map[uint64]time.Time // first time each fed offset >= key was reached
	lines    []string
	tornAt   [][3]uint64
	done     chan struct{}
}

func newSyncRig(t *testing.T, gated bool) *syncRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	r := &syncRig{fedAt: map[uint64]time.Time{}, done: make(chan struct{})}
	r.sess = newRepaintSession()
	r.sess.ring = agent.NewRingBuffer(1 << 20) // hold must fit the ring, as in production
	r.sess.ring.Write([]byte("\x1b[H"))
	r.pane = terminal.NewTerminalPane()
	r.pane.SetTaskID("sync2026")
	r.pane.SetSession(r.sess)
	r.pane.SetRect(0, 0, 120, 40)
	r.pane.SetSyncOutputHold(gated)
	r.sim = tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, r.sim.Init())
	t.Cleanup(r.sim.Fini)
	r.sim.SetSize(120, 40)
	r.tr = newTypingTracker(true)
	r.tr.log = func(f string, a ...any) { r.lines = append(r.lines, fmt.Sprintf(f, a...)) } // always called with r.ui held
	r.pump = newRepaintPump(r.done, r.drawFrame, true)                                      // diagnostic: counts wakes
	r.pane.OnSyncHold = r.pump.wakeAfter
	r.sim.Clear()
	r.pane.Draw(r.sim) // bind
	r.tr.reconcile(r.pane)
	r.lastFed = r.sess.TotalWritten()
	t.Cleanup(func() { close(r.done); <-r.pump.stopped })
	return r
}

// drawFrame is what a pump wake costs: one tview frame (clear, pane draw,
// typing reconcile, flush).
func (r *syncRig) drawFrame() {
	r.ui.Lock()
	defer r.ui.Unlock()
	r.sim.Clear()
	r.pane.Draw(r.sim)
	r.sim.Show()
	sess, fed, drawn := r.pane.TakePaint()
	r.frames++
	if fed != r.lastFed {
		r.painting++
		r.lastFed = fed
		for _, s := range r.spans {
			if fed > s[0] && fed < s[1] {
				r.torn++
				r.tornAt = append(r.tornAt, [3]uint64{fed, s[0], s[1]})
				break
			}
		}
	}
	now := time.Now()
	for k := range r.fedAt {
		if fed >= k && r.fedAt[k].IsZero() {
			r.fedAt[k] = now
		}
	}
	r.tr.reconcileFrames([]paneFrame{{r.pane, sess, fed, drawn}})
	r.tr.flushed()
}

func (r *syncRig) subscribe(gated bool) {
	var w interface{ Write([]byte) (int, error) }
	if gated {
		rw := &repaintWriter{pump: r.pump}
		rw.active.Store(true)
		w = rw
	} else {
		w = plainWriter{r.pump}
	}
	r.sess.AddWriterFrom(w, r.sess.TotalWritten())
	go r.pump.run()
}

func blockSpans(b []byte, base uint64) [][2]uint64 {
	var spans [][2]uint64
	h, l := []byte("\x1b[?2026h"), []byte("\x1b[?2026l")
	start := -1
	for i := 0; i+len(h) <= len(b); i++ {
		switch {
		case string(b[i:i+len(h)]) == string(h) && start < 0:
			start = i
		case string(b[i:i+len(l)]) == string(l) && start >= 0:
			spans = append(spans, [2]uint64{base + uint64(start), base + uint64(i+len(l))}) //nolint:gosec // test offsets
			start = -1
		}
	}
	return spans
}

func playFixture(t *testing.T, gated bool) (frames, painting, torn int, wakes uint64, secs float64, echoTotals, echoDone []float64) {
	t.Helper()
	fixture, err := os.ReadFile("terminal/testdata/claude_sync2026_window.bin")
	testutil.NoError(t, err)
	r := newSyncRig(t, gated)
	r.spans = blockSpans(fixture, r.sess.TotalWritten())
	r.subscribe(gated)

	// The agent's stream: ~150–850B reads every 10ms, the way a busy Claude streams.
	t0 := time.Now()
	for off, i := 0, 0; off < len(fixture); i++ {
		n := min(150+(i*7919)%700, len(fixture)-off)
		r.sess.emit(fixture[off : off+n])
		off += n
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond) // let the last frame land
	secs = time.Since(t0).Seconds()
	r.ui.Lock()
	frames, painting, torn, wakes = r.frames, r.painting, r.torn, r.pump.wakeCount.Load()
	if gated && torn > 0 {
		t.Logf("torn paints (fed, span): %v", r.tornAt)
	}
	r.ui.Unlock()

	// Keystroke echo: the agent answers each key with a 2-read frame (4ms, +4ms).
	for range 12 {
		time.Sleep(200 * time.Millisecond) // out of the previous echo window
		r.ui.Lock()
		pressed := time.Now()
		r.pump.noteKey()
		ev := tcell.NewEventKey(tcell.KeyRune, 'x', 0)
		queued := r.tr.forward(r.pane, ev, pressed, "tasks")
		queued()
		end := r.sess.TotalWritten() + uint64(len("\x1b[?2026h\x1b[Hecho-frame-part-one echo-part-two\x1b[?2026l"))
		r.fedAt[end] = time.Time{}
		r.ui.Unlock()
		time.Sleep(4 * time.Millisecond)
		r.sess.emit([]byte("\x1b[?2026h\x1b[Hecho-frame-part-one"))
		time.Sleep(4 * time.Millisecond)
		r.sess.emit([]byte(" echo-part-two\x1b[?2026l"))
		time.Sleep(80 * time.Millisecond)
		r.ui.Lock()
		if at := r.fedAt[end]; !at.IsZero() {
			echoDone = append(echoDone, ms(at.Sub(pressed)))
		}
		r.ui.Unlock()
	}
	re := regexp.MustCompile(`total_ms=([0-9.]+)`)
	r.ui.Lock()
	for _, l := range r.lines {
		if m := re.FindStringSubmatch(l); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			echoTotals = append(echoTotals, v)
		}
	}
	r.ui.Unlock()
	return
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return -1
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// Offline before/after on real Claude bytes. "before" = every write wakes the
// pump and the pane parses whatever has arrived; "after" = the 2026 gate.
func TestSync2026_BeforeAfterMeasurement(t *testing.T) {
	if testing.Short() {
		t.Skip("timing measurement")
	}
	bf, bp, bt, bw, bs, bTot, bDone := playFixture(t, false)
	af, ap, at, aw, as, aTot, aDone := playFixture(t, true)
	t.Logf("before: pump_wakes=%d frames=%d (%.1f/s) content_paints=%d torn_paints=%d | key->first-echo-paint mean=%.1fms (n=%d) key->complete-echo-frame mean=%.1fms",
		bw, bf, float64(bf)/bs, bp, bt, mean(bTot), len(bTot), mean(bDone))
	t.Logf("after:  pump_wakes=%d frames=%d (%.1f/s) content_paints=%d torn_paints=%d | key->first-echo-paint mean=%.1fms (n=%d) key->complete-echo-frame mean=%.1fms",
		aw, af, float64(af)/as, ap, at, mean(aTot), len(aTot), mean(aDone))
	testutil.Equal(t, at, 0)
	if bt == 0 {
		t.Fatal("before-run never painted inside a block; the measurement proves nothing")
	}
	// Frames are cap-bound (25fps) either way on a continuous stream; what the
	// writer-side tracker removes is the wakes for writes wholly inside a block.
	if aw >= bw {
		t.Errorf("after wakes=%d should be fewer than before wakes=%d (writes inside a block must not wake the pump)", aw, bw)
	}
	if len(aDone) == 0 || mean(aDone) > 100 {
		t.Errorf("complete echo frame took %.1fms after the key; the gate must not add latency beyond the frame itself", mean(aDone))
	}
}

// A block whose ?2026l never arrives must still paint at the timeout with no
// further output to wake the pump.
func TestSync2026_LostCloseFlushesViaTimerWake(t *testing.T) {
	r := newSyncRig(t, true)
	r.subscribe(true)
	r.sess.emit([]byte("\x1b[Hbefore"))
	time.Sleep(60 * time.Millisecond)
	r.sess.emit([]byte("\x1b[?2026h\x1b[Hstuck"))
	time.Sleep(60 * time.Millisecond)
	r.ui.Lock()
	heldFed := r.lastFed
	r.ui.Unlock()
	if heldFed >= r.sess.TotalWritten() {
		t.Fatal("open block was not held")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.ui.Lock()
		fed := r.lastFed
		r.ui.Unlock()
		if fed == r.sess.TotalWritten() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("pane stayed frozen on a block whose close never arrived")
}
