## Context

`reattachSupervised` gets an authoritative live set from the still-running supervisor. `reconcileSessions` nevertheless promotes pending/in_review tasks into the orphan set solely because they have durable Hera bindings. `Record` unconditionally saves a whole new ledger entry, `recordIdleTeam` can independently overwrite dead teammates, and `sendBounceSignals` notifies every candidate regardless of prior recovery. The supervisor shutdown snapshot is absent during an ordinary daemon restart because only supervisor cleanup writes it.

## Goals / Non-Goals

**Goals:** Require session-loss evidence, preserve recovery bookkeeping across repeated startup, retain actual later-bounce recovery, and log ignored historical deaths.

**Non-Goals:** No new supervisor wire protocol or durable session inventory; no frontend-specific work or changes to disabled-recovery and legacy in-process behavior.

## Decisions

- Use the supervisor shutdown snapshot for stale-status sessions and retain the in_progress fallback for unreconciled interrupted work. Durable bindings establish role ownership, never prior session liveness. Removing binding-only selection is necessary because no existing field distinguishes an ancient dead binding from a newly interrupted stale-status session after a hard crash.
- Keep candidate filtering upstream of both recording and ARGUS_BOUNCED notification. Existing entries for excluded tasks remain byte-for-byte unchanged, including killed_at, attempts, bounce_id and note state. A new proven session loss may replace an older entry after a real resume.
- Restrict dormant team recording to current session-loss evidence; team membership and absence alone cannot prove this bounce killed a worker. Preserve existing team entries when their coordinator is newly interrupted.
- Repeated reconciliation of the same event must not reset attempts or duplicate notification. Tests cover same-event repetition and a genuine later interruption separately.

## Risks / Trade-offs

- Hard supervisor crash without a snapshot and stale pending/in_review status → leave those tasks available for explicit revival; a future durable supervisor live inventory could restore automatic recovery without guessing.
- Replacing an existing entry after a real later loss → require fresh snapshot evidence or a new in_progress transition and cover both in tests.
- Existing snapshots can outlive a failed query → retain them on uncertainty and consume only after successful reconciliation, as today.
