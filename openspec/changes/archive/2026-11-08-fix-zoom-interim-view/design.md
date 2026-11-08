## Decisions

- **Authored width source.** `InitialPTYSize` via an optional interface assertion on the session handle (every real handle implements it), not a new method on `agentview.TerminalAdapter`, which would force every fake to change. The `.size` sidecar is not used for live sessions: it follows resizes.
- **Direction.** Emulating wider than the content is only padding; emulating narrower autowraps. Taking the max is safe in both directions, and growing past the initial width still follows the live PTY.
- **Zoom dwell.** Ctrl+Z is a discrete action; 100ms only collapses an accidental double-tap. The rail-traversal dwell (300ms) is unchanged.
- **Backoff.** Cap 15s and reset on zoom. Cost is one IsIdle/InitialPTYSize decision per deferred pane per 15s.
- **Busy agents.** Never force. The clipped interim view is the answer for a coordinator that never idles.
