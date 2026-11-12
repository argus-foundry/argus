package terminal

import "github.com/argus-foundry/argus/internal/app/agentview"

// ReResolveOutcome reports what ReResolveSession did.
type ReResolveOutcome int

const (
	// ReResolveNone: nothing changed (live handle already bound, no session
	// available, or only the same/dead handle was offered).
	ReResolveNone ReResolveOutcome = iota
	// ReResolveLateBound: the pane had no session and one was attached.
	ReResolveLateBound
	// ReResolveReplaced: a dead handle was swapped for a live, different one.
	ReResolveReplaced
)

// ReResolveSession (re)resolves the live session for a task-bound pane — the
// ONE rule shared by the Hera tab's reconcileOne and the Tasks tab's tick/Enter
// (show-disconnected-agents D4), so the two tabs cannot drift.
//
//   - LATE BIND: no session yet → attach whatever resolve yields.
//   - DEAD HANDLE (BUG-013): the bound handle is dead (stream torn down by a
//     daemon bounce while the agent PROCESS lives) → replace it ONLY by a live,
//     DIFFERENT handle. Nothing available (process really gone → log replay) or
//     the same dead handle (client cache not yet evicted) leaves the pane
//     untouched so the emulator is never needlessly reset.
//
// A live bound session is never touched. MUST run on the tview main goroutine
// (SetSession is main-goroutine-only).
func (tp *TerminalPane) ReResolveSession(taskID string, resolve func(string) agentview.TerminalAdapter) ReResolveOutcome {
	if taskID == "" || resolve == nil {
		return ReResolveNone
	}
	cur := tp.Session()
	if cur != nil && cur.Alive() {
		return ReResolveNone
	}
	sess := resolve(taskID)
	if sess == nil {
		return ReResolveNone
	}
	if cur != nil && (sess == cur || !sess.Alive()) {
		return ReResolveNone
	}
	// ResetVT before SetSession: a genuinely different handle is "new content
	// incoming" (recycle kill+respawn as much as a StreamLost re-dial), so any
	// emulator/replay/scroll-anchor state from the OLD handle must not survive.
	tp.ResetVT()
	tp.SetSession(sess)
	tp.ForceResyncPTY()
	if cur == nil {
		return ReResolveLateBound
	}
	return ReResolveReplaced
}
