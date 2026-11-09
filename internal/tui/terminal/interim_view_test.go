package terminal

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

// initialSizeAdapter adds the optional InitialPTYSize accessor every real
// session handle (agent.Session, daemon RemoteSession, apiclient.Session)
// implements, on top of the test mockAdapter.
type initialSizeAdapter struct {
	*mockAdapter
	initCols, initRows int
}

func (a *initialSizeAdapter) InitialPTYSize() (int, int) { return a.initCols, a.initRows }

// zoomFixture is a 48KB slice of the real session log of a Hera coordinator
// that was resumed at 282 cols (fullscreen) and then Ctrl+Z'd down to a
// 142-col split pane while a re-render was pending (9a, 2026-11-08).
func zoomFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/zoom_wide_authored.log")
	testutil.NoError(t, err)
	return string(b)
}

// fragmentRows counts painted rows shaped like the reported garbling: a short
// word fragment at the pane's left edge, a wide gap, then more text at the far
// right edge (a wide-authored line autowrapped by a too-narrow emulator).
func fragmentRows(screen string, innerCols int) int {
	n := 0
	for _, line := range strings.Split(screen, "\n") {
		cells := []rune(line)
		if len(cells) < innerCols+1 {
			continue
		}
		row := cells[1 : innerCols+1]
		if row[0] == ' ' || row[innerCols-1] == ' ' || row[innerCols-1] == '│' {
			continue
		}
		left := 0
		for left < len(row) && row[left] != ' ' {
			left++
		}
		gap := 0
		for i := left; i < len(row) && row[i] == ' '; i++ {
			gap++
		}
		if left <= 10 && gap >= 40 {
			n++
		}
	}
	return n
}

func drawZoomedReplay(t *testing.T, sess *initialSizeAdapter, task string) (string, *TerminalPane) {
	t.Helper()
	setupTaskLog(t, task, zoomFixture(t))
	sim := newSim(t, 144, 40)
	tp := NewTerminalPane()
	tp.SetRect(0, 0, 144, 40)
	tp.SetSession(sess)
	tp.SetTaskID(task)
	tp.scrollOffset = 5
	deadline := time.Now().Add(60 * time.Second)
	tp.Draw(sim)
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
	return readScreen(sim), tp
}

func newZoomSession() *initialSizeAdapter {
	return &initialSizeAdapter{
		mockAdapter: &mockAdapter{alive: true, ptyCols: 142, ptyRows: 38, totalWritten: 6, output: []byte("tail\r\n")},
		initCols:    282, initRows: 69,
	}
}

// Real-log regression: after Ctrl+Z shrinks a live session's PTY the scroll
// replay must still emulate at the width the history was authored at.
func TestInterimView_RealLogNarrowedPTYNotGarbled(t *testing.T) {
	if testing.Short() {
		t.Skip("VT-emulates a real log fixture")
	}
	screen, tp := drawZoomedReplay(t, newZoomSession(), "zoomreal")
	tp.mu.Lock()
	cols := tp.replayEmuCols
	tp.mu.Unlock()
	testutil.Equal(t, cols, 282)
	testutil.Equal(t, fragmentRows(screen, 142), 0)
}

// Control: the same fixture emulated at the pane width does show the
// artifact, proving the detector (and the fixture) actually discriminate.
func TestInterimView_FixtureGarblesAtPaneWidth(t *testing.T) {
	if testing.Short() {
		t.Skip("VT-emulates a real log fixture")
	}
	sess := newZoomSession()
	sess.initCols, sess.initRows = 0, 0 // no authored-width signal → pane width
	screen, _ := drawZoomedReplay(t, sess, "zoomctl")
	if n := fragmentRows(screen, 142); n < 3 {
		t.Fatalf("control: expected garbled fragment rows at pane width, got %d", n)
	}
}

func TestReplayEmuDims_LiveUsesInitialWidthAfterShrink(t *testing.T) {
	s := newZoomSession()
	c, r := replayEmuDims(s, "", 142, 38)
	testutil.Equal(t, c, 282)
	testutil.Equal(t, r, 69)
	// Growing past the initial width still follows the live PTY.
	s.ptyCols = 300
	c, _ = replayEmuDims(s, "", 300, 38)
	testutil.Equal(t, c, 300)
	// A handle without InitialPTYSize keeps the old behaviour.
	c, _ = replayEmuDims(&mockAdapter{alive: true, ptyCols: 142, ptyRows: 38}, "", 142, 38)
	testutil.Equal(t, c, 142)
}

// The live fresh-bind path shares replayEmuDims: a session whose PTY already
// shrank to the pane (current 40) but whose history was authored at 100 must
// replay at 100 and only then resize down, so right-edge cursor jumps are not
// clamped into a stranded column.
func TestInterimView_LiveFreshBindUsesInitialWidth(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("left text row\x1b[90GZ\r\n")
	}
	content := b.String()
	setupTaskLog(t, "liveinit", content)
	sess := &initialSizeAdapter{
		mockAdapter: &mockAdapter{
			alive: true, ptyCols: 40, ptyRows: 20,
			totalWritten: uint64(len(content)), //nolint:gosec // small fixture
			output:       []byte(content),
		},
		initCols: 100, initRows: 48,
	}
	tp := NewTerminalPane()
	tp.SetTaskID("liveinit")
	tp.ResetVT()
	tp.SetSession(sess)
	tp.ForceResyncPTY()
	sim := newSim(t, 42, 22)
	tp.SetRect(0, 0, 42, 22)
	tp.Draw(sim)
	for _, line := range strings.Split(readScreen(sim), "\n")[1:21] {
		if cells := []rune(line); len(cells) > 40 && cells[40] == 'Z' {
			t.Fatal("stranded right-edge Z: fresh bind built at the shrunk PTY width")
		}
	}
}
