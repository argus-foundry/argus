## Context

Completion commonly precedes poller observation of a merged PR. An unread self-assessment prompt then blocks archive even though completion already answered it.

## Goals / non-goals

**Goals:** Suppress obsolete prompts and preserve fail-closed, atomic archive behavior and real coordinator mail.

**Non-Goals:** No text-based provenance inference, changes to coordinator nudges, or frontend contract changes. All three clients use the daemon behavior.

## Decisions

Persist a dedicated message-ID provenance table in the same transaction as each daemon role merge notice. The send transaction re-reads task completion/archive state, so stale poll snapshots cannot send obsolete prompts. Delivery occurs only after commit.

Use the same SQL unread predicate for archive preflight and transaction; only marked merge prompts are excluded after the task is confirmed complete. Stamp those messages read in the archive transaction, rolling the stamp back on any later refusal. Ordinary messages, including identical text, remain blockers.

Legacy messages cannot be distinguished from coordinator mail. Explicit operator-attested IDs are repaired separately; no blanket migration guesses from text.

## Risks / trade-offs

- Legacy notices lack provenance → require explicit operator identification.
- A message racing archive → database lock serializes send and archive, and the send rechecks task and recipient state.
