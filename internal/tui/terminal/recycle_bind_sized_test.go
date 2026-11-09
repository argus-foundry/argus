package terminal

import (
	"os"
	"strings"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/testutil"
)

// recycleBindFixture is the last 20KB of the real session log of a recycled
// Hera coordinator (2026-11-08): spawned at 140x69, resized to the 88x48 pane
// 9681 bytes before the end. Its .sizes index is `0 140 69` + the 88x48 mark.
func recycleBindFixture(t *testing.T) (content string, markOff int64) {
	t.Helper()
	b, err := os.ReadFile("testdata/hera_recycle_bind_tail.log")
	testutil.NoError(t, err)
	return string(b), int64(len(b) - 9681)
}

// A live fresh bind must replay each log segment at the width/height it was
// authored for. Feeding the whole tail in one 140x69 write overlaid two renders
// of Claude's footer (the final message painted over a stale CONTEXT/PWD block,
// old glyphs showing through word gaps). Pane-sized after the bind, exactly one
// footer and an intact final message must remain.
func TestTerminalPane_Draw_LiveFreshBindReplaysSizedSegments(t *testing.T) {
	content, markOff := recycleBindFixture(t)
	setupTaskLog(t, "recyclebind", content)
	agent.ResetSessionSizes("recyclebind", 140, 69)
	agent.AppendSessionSizeMark("recyclebind", markOff, 88, 48)

	sess := &initialSizeAdapter{
		mockAdapter: &mockAdapter{
			alive: true, ptyCols: 88, ptyRows: 48,
			totalWritten: uint64(len(content)), //nolint:gosec // fixture size
			output:       []byte(content),
		},
		initCols: 140, initRows: 69,
	}
	tp := NewTerminalPane()
	tp.SetTaskID("recyclebind")
	tp.ResetVT()
	tp.SetSession(sess)
	tp.ForceResyncPTY()
	sim := newSim(t, 90, 50)
	tp.SetRect(0, 0, 90, 50)
	tp.Draw(sim)

	screen := readScreen(sim)
	testutil.Equal(t, tp.emuCols, 88)
	testutil.Equal(t, strings.Count(screen, "CONTEXT:"), 1)
	testutil.Equal(t, strings.Count(screen, "Cogitated for"), 1)
	if !strings.Contains(screen, "My recommendation: C, with the on-demand re-render key and a setting to restore") {
		t.Fatalf("final message garbled by an overlaid stale footer:\n%s", screen)
	}
}

// The ring-only fallback (no log) has no log offsets, so the width index must
// not be applied to it; the plain single-write path still renders.
func TestReadLiveRebuildHistoryAt_OriginIsLogSpace(t *testing.T) {
	content, _ := recycleBindFixture(t)
	setupTaskLog(t, "recycleorigin", content)
	sess := &mockAdapter{alive: true, totalWritten: uint64(len(content)), output: []byte(content)} //nolint:gosec // fixture size
	raw, _, origin := readLiveRebuildHistoryAt(sess, "recycleorigin")
	testutil.Equal(t, origin, int64(0))
	testutil.Equal(t, len(raw), len(content))

	_, _, origin = readLiveRebuildHistoryAt(sess, "no-such-log")
	testutil.Equal(t, origin, int64(-1))
}

// A lagging log (the ring holds bytes the log has not flushed yet) makes
// readLiveRebuildHistoryAt splice log + ring remainder. The marks are in LOG
// offset space and the spliced bytes are contiguous with the log (verified by
// anchorRingInLog), so origin stays the log head and the sized feed still lands
// every mark on the right byte, including the part that only the ring holds.
func TestTerminalPane_Draw_LiveFreshBindSplicedLogAndRing(t *testing.T) {
	content, markOff := recycleBindFixture(t)
	const lag = 3000 // bytes the ring has that the log does not
	setupTaskLog(t, "recyclesplice", content[:len(content)-lag])
	agent.ResetSessionSizes("recyclesplice", 140, 69)
	agent.AppendSessionSizeMark("recyclesplice", markOff, 88, 48)

	ring := []byte(content[len(content)-(lag+800):]) // 800B overlap: anchorRingInLog scans partial overlaps up to 1024B
	sess := &initialSizeAdapter{
		mockAdapter: &mockAdapter{
			alive: true, ptyCols: 88, ptyRows: 48,
			totalWritten: uint64(len(content)), //nolint:gosec // fixture size
			output:       ring,
		},
		initCols: 140, initRows: 69,
	}

	raw, total, origin := readLiveRebuildHistoryAt(sess, "recyclesplice")
	testutil.Equal(t, origin, int64(0))
	testutil.Equal(t, string(raw), content)
	testutil.Equal(t, total, sess.totalWritten)

	tp := NewTerminalPane()
	tp.SetTaskID("recyclesplice")
	tp.ResetVT()
	tp.SetSession(sess)
	tp.ForceResyncPTY()
	sim := newSim(t, 90, 50)
	tp.SetRect(0, 0, 90, 50)
	tp.Draw(sim)
	screen := readScreen(sim)
	testutil.Equal(t, strings.Count(screen, "CONTEXT:"), 1)
	if !strings.Contains(screen, "My recommendation: C, with the on-demand re-render key and a setting to restore") {
		t.Fatalf("spliced bind garbled:\n%s", screen)
	}
}
