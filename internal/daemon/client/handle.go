package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const defaultBufSize = 256 * 1024 // 256KB ring buffer; session log file handles full scrollback

// pasteEndBytes is the bracketed-paste end sequence (CSI 201~). The inputLoop
// drain treats it as a flush boundary so two back-to-back paste cycles never
// get merged into one PTY write — merging lets the receiving terminal's
// parser confuse them for a single paste, dropping the first cycle's
// contents (visible as only the last of N drag-dropped files registering
// in an interactive CLI).
var pasteEndBytes = []byte("\x1b[201~")

// Compile-time assertion.
var _ agent.SessionHandle = (*RemoteSession)(nil)

// RemoteSession implements agent.SessionHandle by proxying to the daemon.
type RemoteSession struct {
	// streamAttempt is the connectStream retry index of the in-flight stream
	// connection (0 = first dial). Only touched by the connectStream goroutine.
	streamAttempt int

	taskID string
	client *Client

	mu                 sync.Mutex
	buf                *agent.RingBuffer // local ring buffer, populated by stream reader
	writers            []io.Writer       // stream reader tees output to all attached writers (see fan-out note below)
	pidBoundWriters    map[io.Writer]struct{}
	pid                int
	streamBase         uint64
	processDone        chan struct{}
	info               daemon.SessionInfo // cached session info
	done               chan struct{}      // closed when stream EOF
	closeOnce          sync.Once          // guards close(done) — see close()
	inputCh            chan inputItem     // async input channel for WriteInput
	lastInput          time.Time          // wall-clock time of last input write (either origin)
	lastUserInput      time.Time          // wall-clock time of last agentview.OriginUser write — BUG-034 clear-on-input source
	composerTyped      bool               // user typed since the last submit through THIS handle (see ComposerDirty)
	composerGeneration uint64             // advances with recorded user input
}

// ComposerDirty reports whether the user has typed through this handle since
// the last submit (kick-timing-keep-place): a conservative stand-in for "the
// agent's composer holds unsent text" for backends with no structured draft
// report. Tracked client-side like LastUserInput, so typing done in the web
// app, another TUI, or through an earlier handle for the same task is not seen.
func (rs *RemoteSession) ComposerDirty() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.composerTyped
}

// inputItem is one queued WriteInput call awaiting its RPC.
type inputItem struct {
	data   []byte
	origin agentview.InputOrigin
	ctx    context.Context
	ack    chan error
}

func newRemoteSession(taskID string, c *Client) *RemoteSession {
	rs := &RemoteSession{
		taskID:  taskID,
		client:  c,
		buf:     agent.NewRingBuffer(defaultBufSize),
		done:    make(chan struct{}),
		inputCh: make(chan inputItem, 64),
	}
	go rs.inputLoop()
	return rs
}

// inputLoop drains the input channel and sends coalesced bytes to the daemon
// via RPC, one Origin per RPC call. Runs until the done channel is closed.
//
// carry holds an item drainInput already dequeued but could not merge into
// the batch just sent (an origin boundary) — inputLoop must send it next
// instead of blocking on <-rs.inputCh, or it would be lost.
func (rs *RemoteSession) inputLoop() {
	var carry *inputItem
	for {
		var item inputItem
		if carry != nil {
			// carry is unconditionally overwritten by drainInput below, so
			// clearing it here would be an ineffectual assignment.
			item = *carry
		} else {
			select {
			case item = <-rs.inputCh:
			case <-rs.done:
				return
			}
		}
		var batch inputItem
		batch, carry = drainInput(item, rs.inputCh)
		if batch.ack != nil && batch.ctx.Err() != nil {
			batch.ack <- batch.ctx.Err()
			continue
		}
		var resp daemon.StatusResp
		err := rs.client.call("Daemon.WriteInput", &daemon.WriteReq{
			TaskID: rs.taskID,
			Data:   batch.data,
			Origin: batch.origin,
		}, &resp)
		if err == nil && resp.Error != "" {
			err = fmt.Errorf("%s", resp.Error)
		}
		if batch.ack != nil {
			if err == nil && !resp.OK {
				err = fmt.Errorf("input write was not acknowledged")
			}
			batch.ack <- err
		}
		if err != nil {
			uxlog.Log("[client] inputLoop WriteInput failed: task=%s err=%v", rs.taskID, err)
		}
	}
}

// drainInput coalesces additional pending items from ch into initial,
// returning the combined batch ready for one RPC plus an optional carry item
// that could not be merged in (returned, not lost, for the next RPC). Drain
// stops as soon as any of: (a) the channel has no immediately-available
// item, (b) the batch ends with a bracketed-paste end sequence — coalescing
// across that boundary risks merging two `\x1b[200~..\x1b[201~` cycles into
// one PTY write, which the receiver may parse as a single paste — (c) the
// next queued item has a DIFFERENT origin than the batch being built: origin
// is a per-RPC attribute (WriteReq.Origin), so merging a System-origin write
// and a User-origin write into one call would misattribute one of them —
// e.g. a hera bounce instruction queued back-to-back with a real keystroke
// must not stamp the keystroke's bytes (or vice versa) with the wrong origin,
// or (d) either item requires an individual acknowledgement.
func drainInput(initial inputItem, ch <-chan inputItem) (batch inputItem, carry *inputItem) {
	if initial.ack != nil {
		return initial, nil
	}
	buf := initial.data
	for !bytes.HasSuffix(buf, pasteEndBytes) {
		select {
		case more := <-ch:
			if more.origin != initial.origin || more.ack != nil {
				return inputItem{data: buf, origin: initial.origin}, &more
			}
			buf = append(buf, more.data...)
		default:
			return inputItem{data: buf, origin: initial.origin}, nil
		}
	}
	return inputItem{data: buf, origin: initial.origin}, nil
}

func (rs *RemoteSession) PID() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.pid
}

// WriteInput enqueues p onto the input channel; inputLoop drains the channel
// and sends one or more RPCs to the daemon. origin is carried on the wire via
// WriteReq.Origin (see drainInput for why differently-origined items are
// never coalesced into the same RPC) and, locally, decides whether
// lastUserInput advances (see agentview.InputOrigin).
//
// Invariant relied on by drainInput: only PasteHandler writes data ending in
// the bracketed-paste end sequence (\x1b[201~). drainInput uses that suffix
// as a flush boundary so two back-to-back paste cycles never get coalesced
// into one PTY write. Any future caller that writes bracketed-paste content
// must wrap the whole cycle in a single WriteInput call (start sequence,
// payload, and end sequence) — never split it across calls.
func (rs *RemoteSession) WriteInput(p []byte, origin agentview.InputOrigin) (int, error) {
	// Copy so the caller can reuse the slice.
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case rs.inputCh <- inputItem{data: cp, origin: origin}:
		rs.recordInput(cp, origin, nil)
		return len(p), nil
	case <-rs.done:
		return 0, fmt.Errorf("session closed")
	}
}

// WriteInputAcknowledged shares the normal input queue, but waits for the
// supervisor to confirm this individual PTY write.
func (rs *RemoteSession) WriteInputAcknowledged(ctx context.Context, p []byte, origin agentview.InputOrigin) (int, error) {
	rs.mu.Lock()
	composerGeneration := rs.composerGeneration
	rs.mu.Unlock()
	item := inputItem{data: bytes.Clone(p), origin: origin, ctx: ctx, ack: make(chan error, 1)}
	select {
	case rs.inputCh <- item:
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-rs.done:
		return 0, fmt.Errorf("session closed")
	}
	select {
	case err := <-item.ack:
		if err != nil {
			return 0, err
		}
		rs.recordInput(item.data, item.origin, &composerGeneration)
		return len(p), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-rs.done:
		return 0, fmt.Errorf("session closed")
	}
}

func (rs *RemoteSession) recordInput(data []byte, origin agentview.InputOrigin, queuedGeneration *uint64) {
	now := time.Now()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.lastInput = now
	if origin == agentview.OriginUser {
		rs.lastUserInput = now
		// An earlier acknowledgement must not clear a newer typed draft.
		if queuedGeneration == nil || *queuedGeneration == rs.composerGeneration {
			rs.composerTyped = agentview.ComposerTyped(rs.composerTyped, data)
		}
		rs.composerGeneration++
	}
}

// LastInput returns the wall-clock time of the most recent WriteInput call,
// or the zero time if WriteInput has never been called for this session.
//
// Tracked client-side so the SessionHandle interface contract holds, but
// note: the idle-push watcher runs in the daemon process against the
// in-process *agent.Session — so the value RemoteSession returns here is not
// what the watcher reads. It is correct for any local consumer that calls
// WriteInput on this handle.
func (rs *RemoteSession) LastInput() time.Time {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.lastInput
}

// LastUserInput returns the wall-clock time of the most recent WriteInput
// call made through this handle with agentview.OriginUser, or zero. A call
// made with agentview.OriginSystem does not advance it. Like LastInput, this
// is tracked client-side to satisfy the SessionHandle contract; the daemon
// watcher reads the in-process session.
func (rs *RemoteSession) LastUserInput() time.Time {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.lastUserInput
}

func (rs *RemoteSession) Resize(rows, cols uint16) error {
	return rs.resize("Daemon.Resize", rows, cols, 0)
}

// ResizeForPID asks the owner to resize only the identified process.
func (rs *RemoteSession) ResizeForPID(rows, cols uint16, expectedPID int) error {
	if expectedPID <= 0 {
		return agent.ErrSessionNotFound
	}
	return rs.resize("Daemon.ResizeForPID", rows, cols, expectedPID)
}

func (rs *RemoteSession) resize(method string, rows, cols uint16, expectedPID int) error {
	var resp daemon.StatusResp
	err := rs.client.call(method, &daemon.ResizeReq{
		TaskID:      rs.taskID,
		Rows:        rows,
		Cols:        cols,
		ExpectedPID: expectedPID,
	}, &resp)
	if err != nil {
		if expectedPID > 0 && strings.Contains(err.Error(), "can't find method Daemon.ResizeForPID") {
			return agent.ErrSessionCoordinationUnsupported
		}
		return err
	}
	if expectedPID > 0 {
		switch resp.Error {
		case agent.ErrSessionNotFound.Error():
			return agent.ErrSessionNotFound
		case agent.ErrSessionStarting.Error():
			return agent.ErrSessionStarting
		case agent.ErrSessionCoordinationUnsupported.Error():
			return agent.ErrSessionCoordinationUnsupported
		}
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (rs *RemoteSession) RecentOutput() []byte {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.buf.Bytes()
}

func (rs *RemoteSession) RecentOutputTail(n int) []byte {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.buf.Tail(n)
}

// RecentOutputTailWithTotal mirrors Session.RecentOutputTailWithTotal — atomic
// snapshot of (tail, total) under the local stream-buffer lock. RemoteSession
// is only used by the TUI client (the API server runs in the daemon and talks
// to the in-process Session), so this implementation exists purely to satisfy
// SessionHandle; nothing in the TUI calls it.
func (rs *RemoteSession) RecentOutputTailWithTotal(n int) (tail []byte, total uint64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.buf.Tail(n), rs.buf.TotalWritten()
}

// ProcessOutputTailWithTotal returns a cursor relative to the current PTY.
// This matches the log, which starts over when the owner launches a process.
func (rs *RemoteSession) ProcessOutputTailWithTotal(n int) ([]byte, uint64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.buf.Tail(n), rs.buf.TotalWritten() - rs.streamBase
}

func (rs *RemoteSession) TotalWritten() uint64 {
	return rs.buf.TotalWritten()
}

func (rs *RemoteSession) IsIdle() bool {
	rs.refreshInfo()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.info.Idle
}

func (rs *RemoteSession) Alive() bool {
	select {
	case <-rs.done:
		return false
	default:
		return true
	}
}

func (rs *RemoteSession) PTYSize() (cols, rows int) {
	rs.refreshInfo()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.info.Cols, rs.info.Rows
}

// InitialPTYSize returns the PTY dimensions the session was started with.
// Used by the TUI's narrow-stuck detector to spot sessions that need a
// kill+resume to re-flow their conversation history at a wider size.
func (rs *RemoteSession) InitialPTYSize() (cols, rows int) {
	rs.refreshInfo()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.info.InitialCols, rs.info.InitialRows
}

func (rs *RemoteSession) Done() <-chan struct{} {
	return rs.done
}

var retiredProcess = func() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// ProcessDone also closes when a managed restart switches the active process
// behind this handle, while Done remains open for the attached pane.
func (rs *RemoteSession) ProcessDone(expectedPID int) <-chan struct{} {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.Alive() || rs.pid != expectedPID {
		return retiredProcess
	}
	if rs.processDone == nil {
		rs.processDone = make(chan struct{})
	}
	return rs.processDone
}

func (rs *RemoteSession) Err() error {
	return nil // errors are communicated via DB, not the handle
}

func (rs *RemoteSession) WorkDir() string {
	rs.refreshInfo()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.info.WorkDir
}

func (rs *RemoteSession) Stop() error {
	return rs.client.Stop(rs.taskID)
}

// StopScoped stops the session with the given scope via RPC.
func (rs *RemoteSession) StopScoped(scope agent.StopScope) error {
	return rs.client.StopScoped(rs.taskID, scope)
}

// updateInfo refreshes metadata for the handle's process only.
func (rs *RemoteSession) updateInfo(info daemon.SessionInfo) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.pid != 0 && info.PID != rs.pid {
		return false
	}
	rs.info = info
	if info.PID != 0 {
		rs.pid = info.PID
	}
	return true
}

// refreshInfo fetches session info from the daemon.
func (rs *RemoteSession) refreshInfo() {
	var info daemon.SessionInfo
	if err := rs.client.call("Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: rs.taskID}, &info); err != nil {
		return
	}
	rs.updateInfo(info)
}

// BridgeGeneration is queried off the UI thread by daemon bridge readers.
func (rs *RemoteSession) BridgeGeneration() string {
	var info daemon.SessionInfo
	if err := rs.client.call("Daemon.SessionStatus", &daemon.TaskIDReq{TaskID: rs.taskID}, &info); err != nil {
		return ""
	}
	if rs.updateInfo(info) {
		return info.BridgeGeneration
	}
	return ""
}

// Writer fan-out — the supervisor-client double-proxy (session-supervisor P2).
//
// When the daemon is the *consumer* of this client (supervisor mode), its own
// handleStream registers each TUI stream conn as a writer here and the stream
// reader (stream.go) tees supervisor bytes to them — supervisor.readLoop →
// daemon RemoteSession.buf + writers → TUI conn → x/vt. So these MUST fan out,
// mirroring agent.Session's writer set exactly (replay-then-attach, tolerant
// gap-not-duplicate ordering, errored-writer auto-removal).
//
// For the original TUI consumer (daemon mode, talking to the daemon directly)
// nothing registers writers — the terminalpane polls RecentOutputTail — so the
// fan-out is dormant and behavior is unchanged. The bytes still land in rs.buf
// for those pollers regardless of whether any writer is attached.

// AddWriter registers w to receive output: it replays the full local ring, then
// attaches for live output. Replay is sent BEFORE registering so live bytes
// can't race ahead of the replay (gap-not-duplicate; see Session.AddWriter).
func (rs *RemoteSession) AddWriter(w io.Writer) {
	rs.AddWriterFromTolerant(w, 0)
}

// AddWriterFrom registers w to receive output starting at byte `offset`,
// replaying [offset..currentTotal) under the lock so the live attach happens at
// exactly currentTotal — no gap, no duplicate. w.Write MUST NOT block (the lock
// is held through replay and the stream reader takes the same lock). Mirrors
// Session.AddWriterFrom; used by the daemon API server's SSE /stream channelWriter.
func (rs *RemoteSession) AddWriterFrom(w io.Writer, offset uint64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	currentTotal := rs.buf.TotalWritten()
	if offset < currentTotal {
		gap := currentTotal - offset
		var replay []byte
		if gap > uint64(rs.buf.Len()) { //nolint:gosec // Len() is non-negative
			replay = rs.buf.Bytes()
		} else {
			replay = rs.buf.Tail(int(gap)) //nolint:gosec // gap <= Len() per branch guard
		}
		if len(replay) > 0 {
			if _, err := w.Write(replay); err != nil {
				return
			}
		}
	}
	rs.writers = append(rs.writers, w)
}

// AddWriterFromTolerant replays [offset..currentTotal) OUTSIDE the lock, then
// re-acquires it to attach — so a slow/blocking w.Write (e.g. a net.Conn under
// kernel flow control, like the daemon's TUI stream socket) cannot stall the
// stream reader. Accepts a small gap rather than a duplicate. `offset` of 0
// replays the full ring. Mirrors Session.AddWriterFromTolerant.
func (rs *RemoteSession) AddWriterFromTolerant(w io.Writer, offset uint64) {
	rs.addWriterFromTolerant(w, offset, 0)
}

// AddWriterFromPID registers a stream writer with an offset relative to the
// given process. A replay may block, so verify the identity again before
// registering it to receive live output.
func (rs *RemoteSession) AddWriterFromPID(w io.Writer, offset uint64, expectedPID int) bool {
	return rs.addWriterFromTolerant(w, offset, expectedPID)
}

func (rs *RemoteSession) addWriterFromTolerant(w io.Writer, offset uint64, expectedPID int) bool {
	rs.mu.Lock()
	currentTotal := rs.buf.TotalWritten()
	if expectedPID > 0 {
		if rs.pid != expectedPID || !rs.Alive() {
			rs.mu.Unlock()
			return false
		}
		currentTotal -= rs.streamBase
	}
	var replay []byte
	if offset < currentTotal {
		gap := currentTotal - offset
		if gap > uint64(rs.buf.Len()) { //nolint:gosec // Len() is non-negative
			replay = rs.buf.Bytes()
		} else {
			replay = rs.buf.Tail(int(gap)) //nolint:gosec // gap <= Len() per branch guard
		}
	}
	rs.mu.Unlock()

	if len(replay) > 0 {
		if _, err := w.Write(replay); err != nil {
			return false
		}
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()
	if expectedPID > 0 && (rs.pid != expectedPID || !rs.Alive()) {
		return false
	}
	rs.writers = append(rs.writers, w)
	if expectedPID > 0 {
		if rs.pidBoundWriters == nil {
			rs.pidBoundWriters = make(map[io.Writer]struct{})
		}
		rs.pidBoundWriters[w] = struct{}{}
	}
	return true
}

// RemoveWriter unregisters a writer. Safe to call concurrently.
func (rs *RemoteSession) RemoveWriter(w io.Writer) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.removeWriterLocked(w)
}

// removeWriterLocked removes a writer from the slice. Caller must hold rs.mu.
func (rs *RemoteSession) removeWriterLocked(w io.Writer) {
	delete(rs.pidBoundWriters, w)
	for i, existing := range rs.writers {
		if existing == w {
			rs.writers = append(rs.writers[:i], rs.writers[i+1:]...)
			return
		}
	}
}

// close shuts down the remote session.
// close is idempotent + concurrency-safe via closeOnce. Multiple goroutines
// race to close a session's done channel: the session's own connectStream
// goroutine on exit/stream-loss, AND Client.Close() iterating every session on
// shutdown (test t.Cleanup, restartDaemon client swap, supervisor-mode daemon
// cleanup). The old `select{<-done: default: close(done)}` was a non-atomic
// check-then-close: two racers both took the default and double-closed → "close
// of closed channel" panic (surfaced under parallel -race load). Mirrors the
// Client.closeOnce fix one level down.
func (rs *RemoteSession) close() {
	rs.closeOnce.Do(func() {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		close(rs.done)
		if rs.processDone != nil {
			close(rs.processDone)
			rs.processDone = nil
		}
	})
}
