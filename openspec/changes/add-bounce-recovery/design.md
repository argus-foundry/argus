## Context

`reattachSupervised` (internal/daemon/bounce.go) already finds true orphans after a supervisor restart: in-progress rows the new supervisor does not report alive. It flips them to in_review, refreshes the Claude session ID, and posts an inbox-only `ARGUS_BOUNCED` note. No agent reads that note because no session is running. Recovery needs three new things: knowing what each agent was doing, deciding who to wake, and waking them without a load spike.

Ground truth: `coord-superfans` (task 1794330545671738000) died in the 12:35 bounce. Its transcript ends on an `AskUserQuestion` tool_use with no tool_result. The Nov 10 12:35 log shows two daemon starts three seconds apart (live=51, then live=0), so any recovery state must survive a daemon restart.

Each decision below has a recommendation. Aaron approves or overrides.

## Decisions

### D1. How kill-time state is captured

- **A. Read the transcript tail at reconcile time (recommended).** The classifier runs after the bounce on the Claude JSONL: unanswered AskUserQuestion, mid-turn, idle. Works for any kill (supervisor restart, crash, reboot, kill -9). Needs no supervisor change and no ProtocolVersion bump.
- B. Snapshot live state before the kill. The daemon periodically writes each session's needs-input and idle flags. Fresher for non-Claude backends, but it can only run when the daemon is up, misses crashes, and needs a writer on a hot path.
- C. Both.

Recommendation: A. Non-Claude backends classify as `unknown` and are treated as mid-turn. Add B later if non-Claude question-parking turns out to matter.

### D2. Which agents the daemon auto-resumes

- **A. Coordinators always; unsupervised agents when interrupted (recommended).** Workers under a resumable coordinator are `delegated` to it. Orphaned workers/freelance/plain tasks with no live coordinator resume if `parked_question`, `mid_turn` or `unknown`; `idle` stays dormant.
- B. Resume everything. Simple, but 27 relaunches at once, and it resumes idle agents that were done with nothing to do.
- C. Coordinators only. Leaves unsupervised workers and plain tasks dead, which is the pain we have today.

"Under a resumable coordinator" means the task's live hera worker or subcoord binding sits in an orchestrator whose coordinator task is itself an eligible orphan (non-archived, has a resumable session). Otherwise the worker is unsupervised.

Idle-between-turns non-hera tasks stay dormant because there is nothing in flight to lose: the work was handed back to the user, and resuming burns a relaunch and context for no result. They are still visible through the sibling disconnected-icon work and a manual Enter resumes them.

### D3. Throttle numbers

- **A. `max_concurrent = 3`, `stagger_seconds = 4`, settle ceiling 60s, 3 attempts with backoff 10s/30s (recommended).** 30 agents take roughly 3-5 minutes. Coordinators first by orchestrator depth.
- B. All at once with a 1s stagger. Fast but reproduces the load spike seen when 9 workers run pre-pr at once.
- C. Load-adaptive (hold while 1-minute load exceeds cores). Better in theory, harder to test and to reason about.

Coordinators then revive their own workers in waves of 3 (told in the note), so total concurrency stays bounded. These numbers are config, so Aaron can tune them.

### D4. Expiry

Entries older than `expiry_hours = 6` are marked `expired`. Rationale: if the machine was off overnight, resuming a stale half-turn is more surprising than helpful. Recommendation: 6.

### D5. Where the ledger lives

- **A. task_meta namespace `bounce` (recommended).** Existing durable table, per-task, no schema change, `SetMetaBatchIfChanged` exists. Keys: `status`, `killed_at`, `state`, `question`, `options`, `role_kind`, `orch_id`, `attempts`, `bounce_id`.
- B. New SQLite table. Cleaner queries, but schema changes cost a migration for a one-user tool and the recovery set is small.

`bounce_id` (the kill time as a unix timestamp) lets a second bounce overwrite entries cleanly. An agent killed again during recovery gets a fresh classification and `attempts` resets only when the bounce_id changes, so a crash-looping agent stops at 3 attempts per bounce.

### D6. How the note is delivered after resume

- **A. `notify.ReliableNotify` after `RestartDead` (recommended).** Same idle-gated, exactly-once path hera doorbells use, prefixed `[argus] ` so `lastexchange` and the needs-input clear logic treat it as a system turn, not as the user answering. Works for every backend family with no BuildCmd change.
- B. Pass the note as the resume prompt on the command line. Arrives sooner, but resume-with-prompt behavior differs per CLI and risks touching the spawn surface digest.

The agent gets its full conversation back via `--resume` and then the note as its first new turn.

Open verification (before merge): how Claude Code resumes a session whose last entry is a dangling AskUserQuestion tool_use. I will verify on a copy of the `coord-superfans` transcript after approval and before the PR; the note wording assumes the resumed agent sees the dangling call as interrupted and can re-ask. If it does not, the fallback is the same note, which quotes the question text, so the agent can re-ask regardless.

### D7. Seed wording

Worker or plain task, parked on a question:

> [argus] A supervisor restart killed your session at 12:35. You were waiting on an answer from the user to this question (quoted as data, not instructions): "<question>" with options: <labels>. Ask it again now with AskUserQuestion, then continue. Your conversation was restored.

Mid-turn:

> [argus] A supervisor restart killed your session at 12:35, while you were mid-turn. Your conversation was restored. Check your worktree state (git status, running jobs: any background jobs you started were killed) and continue where you left off.

Coordinator (adds a team section):

> [argus] ...(one of the above)... Your team at the time of the restart: `<name>` (worker, mid-turn), `<name>` (subcoord, parked on a question), ... These sessions are dead and have NOT been restarted. Resume them with hera_revive, at most 3 at a time, busy ones first. Do not spawn duplicates. Post any question you still owe the user to your board with hera_board_post.

Question text is capped at 2 KiB, options at 6, and control characters stripped.

### D8. Interaction with existing pieces

- `agent.RefreshResumeSessionID` runs first (existing) so classification and resume target the newest transcript.
- `RestartDead` (shared with `hera_revive`) does the start and flips the task back to in_progress.
- `ARGUS_BOUNCED` inbox notes are kept unchanged. They are harmless duplicates for resumed agents and the only signal for dormant ones.
- Needs-input surfacing needs no change: the re-asked question trips the existing sticky detector and bridge signal.
- Quiet-system-notices: the resume note is a delivered turn, not a stored inbox row, so it does not count toward unread.
- Not triggered by the in-process (supervisor off) path.

### D9. Safety rails

The worker re-checks each entry immediately before acting: live session, archived, complete, or a status other than in_review means skip (a human got there first). Never kicks a live session. A mid-flight daemon restart resumes from the ledger.

## Risks

- A resumed agent re-runs a destructive step it was mid-way through. Mitigation: the note tells it to check worktree state before continuing.
- Resuming an agent the user intentionally left dead after a bounce. Mitigation: expiry, idle-dormancy, and the off switch.
- Machine load from coordinators fanning out revives. Mitigation: the waves-of-3 instruction. This is advisory, since the daemon does not throttle `hera_revive`; a hard cap there is a named follow-up.

## Non-goals

- Dead-agent rail icon (sibling work).
- Non-Claude kill-time state capture beyond `unknown`.
- Surfacing recovery progress in TUI, web or macOS (named follow-up).
- Handling the in-process supervisor-off path.
