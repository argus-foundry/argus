package terminal

import (
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// SyncOutputTimeout bounds how long a pane defers painting inside an open DEC
// private mode 2026 (synchronized output) block. Claude Code writes a whole
// frame inside one block, so a healthy block closes within a few milliseconds;
// the timeout only guards a `CSI ?2026l` that never arrives (agent crash, block
// split across a session restart). 150ms matches Alacritty's synchronized-
// update timeout and our own echoWindow: long enough that a slow-but-healthy
// frame under host load still lands whole, short enough that a lost close costs
// the user at most one visibly stale frame, never a frozen pane.
const SyncOutputTimeout = 150 * time.Millisecond

// syncOutputMaxHold caps the bytes held back for an open block. It must stay
// well under the 256KB ring tail renderLive reads, or a long hold would push the
// held start out of the ring and force a lossy full rebuild.
const syncOutputMaxHold = 128 * 1024

// syncSeq is the exact sequence prefix tracked: ESC [ ? 2 0 2 6, followed by
// 'h' (begin) or 'l' (end). Combined forms such as `CSI ?25;2026h` are not
// recognised (Claude Code never emits them); they fall through as ordinary
// bytes, i.e. today's behaviour.
const syncSeq = "\x1b[?2026"

// syncDFA is a zero-allocation byte matcher for syncSeq+{h,l}. m is how many
// bytes of syncSeq are matched so far; open is the current block state. Every
// match starts at ESC and syncSeq contains no other ESC, so a mismatch falls
// back to 1 (byte is ESC) or 0, never anywhere else.
type syncDFA struct {
	m    int
	open bool
}

// step consumes one byte and reports whether it completed a begin (+1) or end
// (-1) sequence; 0 otherwise.
func (d *syncDFA) step(c byte) int {
	if d.m == len(syncSeq) {
		d.m = 0
		switch c {
		case 'h':
			d.open = true
			return 1
		case 'l':
			d.open = false
			return -1
		}
		// not a final we track: fall through to re-evaluate c as a fresh byte
	}
	switch c {
	case syncSeq[d.m]:
		d.m++
	case 0x1b:
		d.m = 1
	default:
		d.m = 0
	}
	return 0
}

// syncHoldLen returns how many TRAILING bytes of in must be withheld from the
// emulator so it never parses a partial synchronized frame: everything from the
// ESC of the `?2026h` that opened a block still open at the end of in, or — when
// no block is open — a trailing partial `ESC[?2026` prefix whose final byte has
// not arrived yet (a sequence split across chunk boundaries). A block that is
// already open when in begins is invisible here by design: renderLive always
// re-feeds from the held start, so in begins at the `h` itself.
func syncHoldLen(in []byte) int {
	var d syncDFA
	openStart := -1
	for i, c := range in {
		switch d.step(c) {
		case 1:
			if openStart < 0 {
				openStart = i - len(syncSeq)
			}
		case -1:
			openStart = -1
		}
	}
	if openStart >= 0 {
		return len(in) - openStart
	}
	return d.m
}

// SyncTracker follows the synchronized-output mode on a pane's raw output
// stream as it is written, so the repaint pump can skip wakes for writes that
// land wholly inside an open block (the pane would hold them anyway). It is only
// an optimisation: the authoritative hold lives in renderLive. A tracker stuck
// open (lost `?2026l`) self-releases after SyncOutputTimeout so it can never
// suppress wakes for a pane that has already flushed.
type SyncTracker struct {
	mu       sync.Mutex
	d        syncDFA
	openedAt time.Time
	now      func() time.Time // test seam; nil = time.Now
}

// Observe feeds a write and reports whether it should wake the repaint pump:
// false only when the write began and ended inside the same open block.
func (t *SyncTracker) Observe(b []byte) (wake bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now
	if t.now != nil {
		now = t.now
	}
	before := t.d.open
	if before && now().Sub(t.openedAt) >= SyncOutputTimeout {
		t.d.open, before = false, false // stuck open: stop suppressing
	}
	for _, c := range b {
		if t.d.step(c) == 1 {
			t.openedAt = now()
		}
	}
	return !before || !t.d.open
}

// syncGate decides how much of an incremental delta the live emulator may parse
// now. total is the session byte count at the END of delta. It returns the
// prefix to feed and the number of trailing bytes withheld. Must be called with
// the pane's UI-goroutine ownership of the sync fields (renderLive).
func (tp *TerminalPane) syncGate(delta []byte, total uint64) (feed []byte, held int) {
	hold := syncHoldLen(delta)
	if hold == 0 || tp.syncDisabled {
		tp.syncHeldAt, tp.syncHeldSince = 0, time.Time{}
		return delta, 0
	}
	now := time.Now
	if tp.syncNow != nil {
		now = tp.syncNow
	}
	t := now()
	at := total - uint64(hold) //nolint:gosec // hold is a non-negative length bounded by the delta
	// absolute offset where the held region starts
	if tp.syncHeldSince.IsZero() || tp.syncHeldAt != at {
		// First sight of this block (or a later block replaced the old one that
		// closed between draws): start its clock and ask for a wake at expiry,
		// since a lost `?2026l` produces no output to wake the pump.
		tp.syncHeldAt, tp.syncHeldSince = at, t
		if tp.OnSyncHold != nil {
			tp.OnSyncHold(SyncOutputTimeout)
		}
	}
	if t.Sub(tp.syncHeldSince) < SyncOutputTimeout && hold <= syncOutputMaxHold {
		return delta[:len(delta)-hold], hold
	}
	tp.syncExpired++
	logSyncTimeout(tp, hold, t.Sub(tp.syncHeldSince))
	tp.syncHeldAt, tp.syncHeldSince = 0, time.Time{}
	return delta, 0
}

// logSyncTimeout is rate limited to one line per 10s per pane with a count of
// the expiries it swallowed; it is never called per frame.
func logSyncTimeout(tp *TerminalPane, held int, waited time.Duration) {
	now := time.Now()
	if tp.syncNow != nil {
		now = tp.syncNow()
	}
	if !tp.syncLogAt.IsZero() && now.Sub(tp.syncLogAt) < 10*time.Second {
		return
	}
	tp.syncLogAt = now
	uxlog.Log("[sync2026] timeout: no ?2026l after %dms (held=%dB), flushing partial frame; expired_total=%d", waited.Milliseconds(), held, tp.syncExpired)
}

// resetSyncGate drops any hold. Called wherever the live emulator is reset or
// the fed cursor is rewritten, so a stale "open" block cannot survive a rebind.
func (tp *TerminalPane) resetSyncGate() {
	tp.syncHeldAt, tp.syncHeldSince = 0, time.Time{}
}

// SetSyncOutputHold enables (default) or disables the DEC 2026 hold. Disabling
// reproduces the pre-gate behaviour and exists only so the offline before/after
// measurement in internal/tui can compare against it. Call before the pane's
// first Draw or from the goroutine that draws it.
func (tp *TerminalPane) SetSyncOutputHold(enabled bool) { tp.syncDisabled = !enabled }
