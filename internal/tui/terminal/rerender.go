package terminal

import (
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// SetRerenderingForTask ignores late outcomes for a pane that has rebound.
func (tp *TerminalPane) SetRerenderingForTask(taskID string, active bool) {
	tp.setRerenderStatus(taskID, active, false)
}

func (tp *TerminalPane) SetRerenderPendingForTask(taskID string) {
	tp.setRerenderStatus(taskID, true, true)
}

func (tp *TerminalPane) setRerenderStatus(taskID string, active, pending bool) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.taskID != taskID || tp.rerendering == active && tp.rerenderPending == pending {
		return
	}
	tp.rerendering = active
	tp.rerenderPending = pending
	uxlog.Log("[terminalpane] rerender status task=%s active=%t", taskID, active)
}

func (tp *TerminalPane) RerenderStatus() string {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if !tp.rerendering {
		return ""
	}
	if tp.rerenderPending {
		return "Re-render pending"
	}
	return "Re-rendering…"
}

// Rerendering reports whether this pane has an outstanding width repair.
func (tp *TerminalPane) Rerendering() bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.rerendering
}

// OwedFunc reports, once per Draw on the UI goroutine, whether the pane's
// history is owed a re-render (resize-without-restart). When owed is false,
// clearReason names why a previously shown notice cleared ("width-returned",
// "rerendered", "setting-on", ...) for the transition log. It must not block.
type OwedFunc func(taskID string, sess agentview.TerminalAdapter, cols int) (owed bool, clearReason string)

// SetOwedFunc wires the owed-notice evaluator and the live key hint (the
// resolved agent.rerender binding; empty means unbound). The evaluator runs only
// on EVENTS (see owedStateForDraw), never once per frame.
func (tp *TerminalPane) SetOwedFunc(fn OwedFunc, hint func() string) {
	tp.mu.Lock()
	tp.owedFn = fn
	tp.owedHintFn = hint
	tp.owedPending = true
	tp.mu.Unlock()
}

// InvalidateOwed marks the owed state stale so the next Draw re-evaluates it
// once. Safe from any goroutine. The App calls it on the discrete events the
// pane cannot observe itself: the setting toggling, a keymap reload (the hint),
// the authored-width anchor finishing its async load, and a session exiting.
// Width changes and binds are detected by the pane (owedStateForDraw / SetSession
// / SetTaskID / ResetVT).
func (tp *TerminalPane) InvalidateOwed() {
	tp.mu.Lock()
	tp.owedPending = true
	tp.mu.Unlock()
}

// RerenderOwed reports whether the owed notice is currently showing.
func (tp *TerminalPane) RerenderOwed() bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.owedShown
}

// owedStateForDraw returns the stored owed state, re-evaluating it ONLY when an
// event made it stale: a pending invalidation (bind/rebind, session change,
// setting, keymap, anchor load) or a changed pane width. A plain repaint at an
// unchanged width does no evaluation at all — no DB, no RPC, no callback — which
// matters because Draw runs at up to ~125 fps under typing. Shown/cleared
// transitions are logged once each. UI goroutine only (called from Draw).
func (tp *TerminalPane) owedStateForDraw(cols int) bool {
	tp.mu.Lock()
	if tp.owedFn == nil || tp.taskID == "" {
		tp.owedShown = false
		tp.mu.Unlock()
		return false
	}
	if !tp.owedPending && cols == tp.owedCols {
		shown := tp.owedShown
		tp.mu.Unlock()
		return shown
	}
	tp.owedPending = false
	tp.owedCols = cols
	fn, hintFn, sess, taskID := tp.owedFn, tp.owedHintFn, tp.session, tp.taskID
	tp.mu.Unlock()

	owed, reason := fn(taskID, sess, cols)
	hint := ""
	if hintFn != nil {
		hint = hintFn()
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.taskID != taskID {
		return false // rebound while evaluating; the rebind re-armed owedPending
	}
	switch {
	case owed && !tp.owedShown:
		uxlog.Log("[rerender] owed notice shown task=%s cols=%d", taskID, cols)
	case !owed && tp.owedShown:
		if reason == "" {
			reason = "cleared"
		}
		uxlog.Log("[rerender] owed notice cleared task=%s reason=%s", taskID, reason)
	}
	tp.owedShown = owed
	tp.owedHint = hint
	return owed
}

// clearOwedLocked drops the notice on rebind, logging the transition if it was
// showing. Caller holds tp.mu.
func (tp *TerminalPane) clearOwedLocked(reason string) {
	if tp.owedShown {
		uxlog.Log("[rerender] owed notice cleared task=%s reason=%s", tp.taskID, reason)
	}
	tp.owedShown = false
	tp.owedPending = true // a rebind is an event: re-evaluate against the new task/session
	tp.owedX, tp.owedY, tp.owedW = 0, 0, 0
}

// owedNoticeLayout picks the longest notice form that fits between the left
// title (+hint) and the right edge, so it can never overlap the title:
// "width changed · <key> re-render", else "<key> re-render", else nothing.
// innerX/innerW are the inner content columns (border row is y-1).
func (tp *TerminalPane) owedNoticeLayout(innerX, innerW int, title string) (text string, startX, w int) {
	tp.mu.Lock()
	key := tp.owedHint // resolved at the last evaluation event, not per frame
	tp.mu.Unlock()
	tail := "re-render via ctrl+k"
	if key != "" {
		tail = key + " re-render"
	}
	left := innerX + utf8.RuneCountInString(title) + utf8.RuneCountInString(tp.borderHint) + 1
	avail := innerX + innerW - left
	for _, cand := range []string{" width changed · " + tail + " ", " " + tail + " "} {
		if n := utf8.RuneCountInString(cand); n <= avail {
			return cand, innerX + innerW - n, n
		}
	}
	return "", 0, 0
}

func (tp *TerminalPane) setOwedRect(x, y, w int) {
	tp.mu.Lock()
	tp.owedX, tp.owedY, tp.owedW = x, y, w
	tp.mu.Unlock()
}

// hitOwed reports whether the screen cell lies on the drawn owed notice.
func (tp *TerminalPane) hitOwed(x, y int) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.owedW > 0 && y == tp.owedY && x >= tp.owedX && x < tp.owedX+tp.owedW
}
