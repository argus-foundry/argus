## Why

A session-supervisor restart ("bounce") kills every agent PTY. On Nov 10 a 12:35 restart orphaned 27 agents and a 17:06 restart orphaned 9. Today the daemon flips their tasks from in_progress to in_review, drops an inbox note nobody reads, and leaves the agents dead. Nothing wakes them. Aaron notices hours later, if at all. A concrete case: coordinator `coord-superfans` died at 12:35 while parked on an unanswered AskUserQuestion ("Question 4 of ~6: where should a superfan run?"). Its transcript ends on that tool call, and it sat dead until someone pressed Enter.

## What Changes

- At the supervisor-restart reconcile, the daemon records every true orphan in a durable recovery ledger: when it died, what it was doing (parked on a question, mid-turn, or idle between turns), the pending question text, and its hera role.
- A new recovery worker auto-resumes, in throttled order, the agents that were actually interrupted: coordinators first, then agents with no live coordinator above them. Each resumed agent receives an `[argus]` system note saying a supervisor restart killed it, what it was doing, and (for coordinators) which of its workers and sub-coordinators were killed and in what state.
- Coordinators are told to resume their own team with `hera_revive` in small waves. The daemon does not blindly restart supervised workers.
- An agent killed on a question is told to re-ask it. The existing needs-input detection then surfaces it.
- Idle-between-turns agents with no coordinator stay dormant. Residents are excluded (they have their own ensure path).
- New `[bounce_recovery]` config: on/off switch (off restores today's behavior exactly), concurrency cap, stagger, and expiry.
- No supervisor-protocol change. State is read from the Claude transcript after the fact, so no ProtocolVersion bump or spawn-surface digest re-record is needed.

## Capabilities

### New Capabilities

- `bounce-recovery`: the recovery ledger, state classification, resume policy, throttling, and the resume note.

### Modified Capabilities

- `daemon-lifecycle`: the supervisor-restart reconcile also records orphans in the recovery ledger.

## Impact

- New `internal/transcriptstate` (pure transcript-tail classifier), `internal/bouncerecovery` (ledger, planner, note builder, worker), and a small hook in `internal/daemon/bounce.go`.
- New `[bounce_recovery]` section in `internal/config`.
- Reuses `hera.ReviveRole` and `RestartDead`, `agent.RefreshResumeSessionID`, `notify.ReliableNotify`, the `[argus] ` system-turn marker, and `task_meta` for the ledger.
- Frontend parity: daemon-only behavior. Nothing new on the REST surface, so TUI, web, and macOS are unchanged. Showing recovery status (for example "waiting to resume") in the clients is a named follow-up, and the sibling `disconnected-icon` work covers the dead-agent glyph.
