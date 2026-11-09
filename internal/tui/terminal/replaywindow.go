package terminal

import (
	"bytes"
	"io"
	"os"
	"strings"

	xvt "github.com/charmbracelet/x/vt"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/oscfilter"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Sliding replay window (bound-tui-emulator-memory).
//
// A replay emulator holds at most replayScrollbackCellBudget cells, so it can
// only show a WINDOW [start, end) of the session log. The window slides:
// scrolling past its top rebuilds it further back, scrolling down to its
// bottom rebuilds it forward again, so every byte of history stays reachable
// at bounded memory. A window is built by feeding the log FORWARD from start in
// replayFeedChunk pieces until the cell budget is reached (or EOF), so its
// bottom edge is a real terminal state, never a spliced seam.

// replayFeedChunk is the per-Write slice fed to a replay emulator. Small
// enough that the budget check overshoots by at most ~one chunk. A var so
// tests can force many chunk boundaries; production never mutates it.
var replayFeedChunk = 1 << 20

const (
	// replayAnchorPad widens the line range searched for the content anchor
	// around the checkpoint-derived estimate; the old and new windows start at
	// different mid-stream points, so their line numbering drifts slightly.
	replayAnchorPad = 300

	// replayAnchorSkipTop is how many rows at the very top of a window are
	// excluded from the content signature: a window starts mid-stream, so its
	// first rows are typically a cut-off fragment that the neighbouring window
	// renders whole and a text match would never find.
	replayAnchorSkipTop = 3

	// replayAnchorMinSig is the minimum non-space characters a row signature
	// must carry before content matching is trusted over the estimate.
	replayAnchorMinSig = 12
)

// replayCheckpoint records that after feeding the log through byte offset off
// the emulator's scrollback held `lines` lines. A sorted table of these maps a
// scrollback line to a log byte range (and back) at chunk granularity.
type replayCheckpoint struct {
	off   int64
	lines int
}

// deadEndCache memoizes deadLogEnd for one (task, log size).
type deadEndCache struct {
	taskID    string
	size, end int64
}

type replayKick int

const (
	kickNone    replayKick = iota
	kickExtend             // legacy BUG-E: read further back, still ending at EOF
	kickBack               // slide the budget-limited window further back
	kickForward            // slide the window forward toward the log tail
)

// viewAnchor identifies the content at the top of the viewport independently
// of any one emulator: a log byte range plus the row texts themselves.
type viewAnchor struct {
	offLo, offHi int64
	sig          []string
	// eff is the scroll offset the anchor row was captured at: the requested
	// offset clamped to the old window's real range. Any excess the user
	// scrolled past the window edge is preserved by the slide, not lost.
	eff int
	// sigSkip is how many rows below the anchor row the signature starts
	// (replayAnchorSkipTop rows of a window top are unreliable).
	sigSkip int
}

// replayWindowReq asks asyncReplayRebuildAt for a slid window.
type replayWindowReq struct {
	forward    bool
	start      int64
	anchor     viewAnchor
	kickScroll int // scrollOffset when the rebuild was kicked
	viewport   int
}

// replayKickLocked decides what rebuild, if any, the current scroll position
// needs. Callers hold tp.mu.
func (tp *TerminalPane) replayKickLocked(cols, rows, offset, viewport int) replayKick {
	if tp.replayEmu == nil || tp.replayEmuCols != cols || tp.replayEmuRows != rows || offset <= 0 {
		return kickNone
	}
	if tp.replayEmuFirstByte > 0 {
		// A window starts mid-stream, so its top rows are a cut-off fragment.
		// Slide back as soon as the viewport reaches them (not only once the
		// user scrolls past the top), so those rows are never on display while
		// older history exists.
		if tp.replayEmuAtBudget && offset > tp.replayEmuMaxScroll-replayAnchorSkipTop {
			return kickBack
		}
		if !tp.replayEmuAtBudget && offset > tp.replayEmuMaxScroll {
			return kickExtend
		}
	}
	if tp.replayEmuNonTail && offset < viewport {
		return kickForward
	}
	return kickNone
}

// resetReplayWindowLocked clears the sliding-window state alongside the other
// replayEmu* fields. Callers hold tp.mu.
func (tp *TerminalPane) resetReplayWindowLocked() {
	tp.replayEmuAtBudget = false
	tp.replayEmuNonTail = false
	tp.replayEmuEnd = 0
	tp.replayEmuCheckpoints = nil
	tp.replayPendingShiftSet = false
}

// scrollbackCells totals the cells held in emu's scrollback.
func scrollbackCells(emu *xvt.SafeEmulator) int {
	cells := 0
	if sb := emu.Scrollback(); sb != nil {
		for _, l := range sb.Lines() {
			cells += len(l)
		}
	}
	return cells
}

// feedReplayChunks feeds src[start:limit) to emu in replayFeedChunk pieces
// through the same filter pipeline a one-shot build uses (ESC-boundary
// alignment of the first chunk, scroll-region clamp, stateful OSC filter).
// Chunks are cut just before an ESC so no escape sequence is split across
// Writes (ClampScrollRegion works per call). When budgetCells > 0 feeding stops
// once the scrollback reaches it. origin is added to the reported offsets
// (callers feeding an in-memory tail pass the tail's log offset). It returns
// the offset fed through and the checkpoint table, which starts with a
// {start,0} sentinel.
func feedReplayChunks(emu *xvt.SafeEmulator, src io.ReaderAt, start, limit, origin int64, cols, rows, budgetCells int, align bool) (int64, []replayCheckpoint) {
	return feedReplayChunksSized(emu, src, start, limit, origin, cols, rows, budgetCells, align, nil)
}

// feedReplayChunksSized is feedReplayChunks with the session's width index
// (agent.LoadSessionSizes; offsets in the same space as origin+pos). Each
// segment is emulated at the PTY size it was authored for: the emulator is
// resized to the size in force at start, and again at every mark, so cursor
// math that depends on wrap counts (an agent's relative cursor-up redraw)
// lands where it did on the real PTY. emu is built at (cols, rows) — the
// widest size, which every caller's paint/anchor math assumes — and is
// resized back to exactly that after the last chunk. x/vt Resize crops the
// visible rows on narrowing and keeps scrollback lines at their full width,
// the same as the live emulator saw. nil/empty marks is a plain single-width
// feed.
func feedReplayChunksSized(emu *xvt.SafeEmulator, src io.ReaderAt, start, limit, origin int64, cols, rows, budgetCells int, align bool, marks []agent.SizeMark) (int64, []replayCheckpoint) {
	cps := []replayCheckpoint{{off: origin + start, lines: 0}}
	var osc oscfilter.Filter
	pos := start
	buf := make([]byte, replayFeedChunk)
	first := true
	curCols, curRows := cols, rows
	nextMark := 0
	resizes := 0
	apply := func(m agent.SizeMark) {
		if m.Cols == curCols && m.Rows == curRows {
			return
		}
		curCols, curRows = m.Cols, m.Rows
		emu.Resize(curCols, curRows)
		resizes++
	}
	if len(marks) > 0 {
		if m, ok := agent.SizeMarkAt(marks, origin+start); ok {
			apply(m)
		}
		for nextMark < len(marks) && marks[nextMark].Off <= origin+start {
			nextMark++
		}
	}
	for pos < limit {
		n := int64(len(buf))
		if limit-pos < n {
			n = limit - pos
		}
		got, err := src.ReadAt(buf[:n], pos)
		if got <= 0 {
			break
		}
		chunk := buf[:got]
		if pos+int64(got) < limit {
			if cut := lastIndexByte(chunk, 0x1b); cut > 0 {
				chunk = chunk[:cut]
			}
		}
		// Never let a chunk straddle a width change: cut exactly at the next
		// mark (the parser is stateful, so even a mid-sequence cut is safe).
		if nextMark < len(marks) {
			if rel := marks[nextMark].Off - (origin + pos); rel > 0 && rel < int64(len(chunk)) {
				chunk = chunk[:rel]
			}
		}
		fed := chunk
		if first && align {
			fed = AlignToEscBoundary(fed)
		}
		first = false
		_, _ = SafeEmuWrite(emu, osc.Filter(ClampScrollRegion(fed, curCols, curRows)))
		pos += int64(len(chunk))
		for nextMark < len(marks) && marks[nextMark].Off <= origin+pos {
			apply(marks[nextMark])
			nextMark++
		}
		cps = append(cps, replayCheckpoint{off: origin + pos, lines: emu.ScrollbackLen()})
		if err != nil && err != io.EOF {
			break
		}
		if budgetCells > 0 && scrollbackCells(emu) >= budgetCells {
			break
		}
	}
	if tail := osc.Flush(); len(tail) > 0 {
		_, _ = SafeEmuWrite(emu, tail)
	}
	if len(marks) > 0 {
		if curCols != cols || curRows != rows {
			emu.Resize(cols, rows)
		}
		uxlog.Log("[terminalpane] replay segments: marks=%d resizes=%d fed=[%d,%d) final=%dx%d", len(marks), resizes, origin+start, origin+pos, cols, rows)
	}
	return origin + pos, cps
}

func lastIndexByte(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// checkpointsAfterTrim rebases cps after the oldest `dropped` lines were
// trimmed from the emulator, returning the rebased table and the (conservative,
// chunk-granular) log offset the retained scrollback now starts at.
func checkpointsAfterTrim(cps []replayCheckpoint, dropped int) ([]replayCheckpoint, int64) {
	if dropped <= 0 || len(cps) == 0 {
		start := int64(0)
		if len(cps) > 0 {
			start = cps[0].off
		}
		return cps, start
	}
	startOff := cps[0].off
	i := 0
	for i < len(cps) && cps[i].lines <= dropped {
		startOff = cps[i].off
		i++
	}
	out := make([]replayCheckpoint, 0, len(cps)-i+1)
	out = append(out, replayCheckpoint{off: startOff, lines: 0})
	for ; i < len(cps); i++ {
		out = append(out, replayCheckpoint{off: cps[i].off, lines: cps[i].lines - dropped})
	}
	return out, startOff
}

// byteRangeForLine maps a scrollback line index to the log byte range that
// produced it (chunk granularity). end is the window's end offset, used for
// lines past the last checkpoint (the screen region).
func byteRangeForLine(cps []replayCheckpoint, line int, end int64) (lo, hi int64) {
	if len(cps) == 0 {
		return 0, end
	}
	lo, hi = cps[0].off, end
	for _, cp := range cps {
		if cp.lines <= line {
			lo = cp.off
			continue
		}
		hi = cp.off
		break
	}
	return lo, hi
}

// lineRangeForBytes is the inverse map: the scrollback line range covering the
// log byte range [lo, hi]. total is the window's total line count, the upper
// bound when hi lies at or past the last checkpoint.
func lineRangeForBytes(cps []replayCheckpoint, lo, hi int64, total int) (loLine, hiLine int) {
	hiLine = total
	for _, cp := range cps {
		if cp.off <= lo {
			loLine = cp.lines
		}
	}
	for _, cp := range cps {
		if cp.off >= hi {
			hiLine = cp.lines
			break
		}
	}
	if hiLine < loLine {
		hiLine = loLine
	}
	return loLine, hiLine
}

// unifiedRowText returns row idx of emu in the unified (scrollback then main
// screen) index space paintEmu uses, with trailing blanks trimmed.
func unifiedRowText(emu *xvt.SafeEmulator, sbLen, idx, cols int) string {
	var sb strings.Builder
	if idx < sbLen {
		line := emu.Scrollback().Line(idx)
		for x := 0; x < len(line) && x < cols; x++ {
			c := line[x].Content
			if c == "" {
				c = " "
			}
			sb.WriteString(c)
		}
	} else {
		y := idx - sbLen
		for x := 0; x < cols; x++ {
			cell := emu.CellAt(x, y)
			if cell == nil || cell.Content == "" {
				sb.WriteByte(' ')
				continue
			}
			sb.WriteString(cell.Content)
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// viewGeometry returns paintEmu's unified-space geometry for emu: scrollback
// length and total addressable lines. Mirrors paintEmu's totalLines math.
func viewGeometry(emu *xvt.SafeEmulator, cols, rows int, curVisible bool) (sbLen, total int) {
	sbLen = emu.ScrollbackLen()
	last := FindLastContentRowEmu(emu, cols, rows)
	if curVisible {
		if cur := emu.CursorPosition(); cur.Y > last {
			last = cur.Y
		}
	}
	return sbLen, sbLen + last + 1
}

// anchorTop returns the unified index of the top visible row for a scroll
// offset (clamped like paintEmu does) and the geometry it was derived from.
func anchorTop(emu *xvt.SafeEmulator, cols, rows, offset, h int, curVisible bool) (top, eff, sbLen, total int) {
	sbLen, total = viewGeometry(emu, cols, rows, curVisible)
	maxScroll := total - h
	if maxScroll < 0 {
		maxScroll = 0
	}
	if offset > maxScroll {
		offset = maxScroll
	}
	end := total - 1 - offset
	top = end - h + 1
	if top < 0 {
		top = 0
	}
	return top, offset, sbLen, total
}

// captureAnchor snapshots what the viewport top shows so a different window
// can find the same content. Returns ok=false when the old window carries no
// checkpoint table (no log-backed build).
func captureAnchor(emu *xvt.SafeEmulator, cps []replayCheckpoint, end int64, cols, rows, offset, h int, curVisible bool) (viewAnchor, bool) {
	if emu == nil || len(cps) == 0 {
		return viewAnchor{}, false
	}
	top, eff, sbLen, total := anchorTop(emu, cols, rows, offset, h, curVisible)
	lo, hi := byteRangeForLine(cps, top, end)
	a := viewAnchor{offLo: lo, offHi: hi, eff: eff}
	chars := 0
	if top < replayAnchorSkipTop {
		a.sigSkip = replayAnchorSkipTop - top
	}
	for i := top + a.sigSkip; i < total && len(a.sig) < 6; i++ {
		t := unifiedRowText(emu, sbLen, i, cols)
		a.sig = append(a.sig, t)
		chars += len(strings.TrimSpace(t))
		if len(a.sig) >= 3 && chars >= replayAnchorMinSig {
			break
		}
	}
	if chars < replayAnchorMinSig {
		a.sig = nil // too little content to trust a text match
	}
	return a, true
}

// locateAnchor finds the unified line index in emu (a freshly built window with
// checkpoints cps) showing the anchored content, and whether a content match
// confirmed it (false = checkpoint estimate only).
func locateAnchor(emu *xvt.SafeEmulator, cps []replayCheckpoint, a viewAnchor, cols, rows int, curVisible bool) (line int, matched bool) {
	sbLen, total := viewGeometry(emu, cols, rows, curVisible)
	loLine, hiLine := lineRangeForBytes(cps, a.offLo, a.offHi, total)
	est := (loLine + hiLine) / 2
	if len(a.sig) == 0 {
		return est, false
	}
	from, to := loLine-replayAnchorPad, hiLine+replayAnchorPad
	if from < 0 {
		from = 0
	}
	best, bestDist := -1, 0
	for p := from; p <= to && p+len(a.sig) <= total; p++ {
		ok := true
		for k, want := range a.sig {
			if unifiedRowText(emu, sbLen, p+k, cols) != want {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		d := p - est
		if d < 0 {
			d = -d
		}
		if best < 0 || d < bestDist {
			best, bestDist = p, d
		}
	}
	if best < 0 {
		return est, false
	}
	return best - a.sigSkip, true
}

// planWindowLocked builds the rebuild request for a back/forward slide from the
// currently published window. Callers hold tp.mu. Returns nil when no slide is
// possible (no log-backed window).
func (tp *TerminalPane) planWindowLocked(kick replayKick, cols, rows, offset, h int) *replayWindowReq {
	if tp.replayEmu == nil || len(tp.replayEmuCheckpoints) == 0 {
		return nil
	}
	start, end := tp.replayEmuFirstByte, tp.replayEmuEnd
	span := end - start
	if span < int64(replayFeedChunk) {
		span = int64(replayFeedChunk)
	}
	req := &replayWindowReq{forward: kick == kickForward, kickScroll: offset, viewport: h}
	if req.forward {
		req.start = start + span/2
	} else {
		req.start = start - span/2
		if req.start < 0 {
			req.start = 0
		}
		if req.start >= start {
			return nil
		}
	}
	a, ok := captureAnchor(tp.replayEmu, tp.replayEmuCheckpoints, end, cols, rows, offset, h, tp.replayEmuCursorVisible)
	if !ok {
		return nil
	}
	req.anchor = a
	return req
}

// buildWindow feeds req's window into emu from the session log and returns the
// result. limit (0 = the log's end) bounds the window for a dead session whose
// replay stops at its final alt-screen exit. fileSize is the on-disk size, lim
// the effective upper bound the window may reach. ok=false when the log can't
// be read or req.start is already at/after the bound.
func buildWindow(emu *xvt.SafeEmulator, taskID string, req *replayWindowReq, cols, rows int, limit int64) (end, fileSize, lim int64, cps []replayCheckpoint, ok bool) {
	f, err := os.Open(agent.SessionLogPath(taskID))
	if err != nil {
		return 0, 0, 0, nil, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, 0, 0, nil, false
	}
	fileSize = fi.Size()
	lim = fileSize
	if limit > 0 && limit < lim {
		lim = limit
	}
	if lim <= req.start {
		return 0, 0, 0, nil, false
	}
	end, cps = feedReplayChunksSized(emu, f, req.start, lim, 0, cols, rows, replayScrollbackCellBudget, req.start > 0, agent.LoadSessionSizes(taskID))
	uxlog.Log("[terminalpane] replay window %s: start=%d end=%d of %d (file %d, forward=%v, lines=%d, cells=%d)",
		taskID, req.start, end, lim, fileSize, req.forward, emu.ScrollbackLen(), scrollbackCells(emu))
	return end, fileSize, lim, cps, true
}

// deadLogEnd returns the log offset a dead session's replay ends at: the start
// of the LAST alt-screen exit (CSI ?1049l), so the replay freezes on the final
// alt-screen frame instead of the post-teardown resume hint (see
// truncateAtFinalAltScreenExit). It is the whole-log equivalent of that
// function, computed by scanning backward from EOF and cached per
// (task, log size) because a dead session's log no longer changes. No exit, or
// an exit at offset 0 (trimming would discard everything), yields the log size.
// Safe from any goroutine.
func (tp *TerminalPane) deadLogEnd(taskID string) int64 {
	fi, err := os.Stat(agent.SessionLogPath(taskID))
	if err != nil || fi.Size() == 0 {
		return 0
	}
	size := fi.Size()
	tp.mu.Lock()
	if c := tp.deadEnd; c.taskID == taskID && c.size == size {
		tp.mu.Unlock()
		return c.end
	}
	tp.mu.Unlock()

	end := size
	if f, err := os.Open(agent.SessionLogPath(taskID)); err == nil {
		if idx := lastIndexInFile(f, size, altScreenExitSeq); idx > 0 {
			end = idx
		}
		f.Close()
	}
	tp.mu.Lock()
	tp.deadEnd = deadEndCache{taskID: taskID, size: size, end: end}
	tp.mu.Unlock()
	uxlog.Log("[terminalpane] dead-session replay bound for %s: end=%d of %d", taskID, end, size)
	return end
}

// lastIndexInFile returns the offset of the last occurrence of seq in
// f[0:size), or -1. It scans backward in replayFeedChunk-sized blocks that
// overlap by len(seq)-1 so a match straddling a block boundary is found.
func lastIndexInFile(f io.ReaderAt, size int64, seq []byte) int64 {
	if len(seq) == 0 || size < int64(len(seq)) {
		return -1
	}
	overlap := int64(len(seq) - 1)
	block := int64(replayFeedChunk)
	if block <= overlap {
		block = overlap + 1
	}
	buf := make([]byte, block+overlap)
	for hi := size; hi > 0; {
		lo := hi - block
		if lo < 0 {
			lo = 0
		}
		readHi := hi + overlap
		if readHi > size {
			readHi = size
		}
		n, err := f.ReadAt(buf[:readHi-lo], lo)
		if n > 0 {
			if idx := bytes.LastIndex(buf[:n], seq); idx >= 0 {
				return lo + int64(idx)
			}
		}
		if err != nil && err != io.EOF {
			return -1
		}
		hi = lo
	}
	return -1
}
