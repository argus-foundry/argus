## Context

47 sessions died in the November 13 bounce. Status-only reconciliation missed live in_review and pending tasks. A start RPC can time out while the supervisor continues launching; treating that session as somebody else's loses its note.

## Goals / non-goals

**Goals:** Cover the four approved recovery fixes with regression tests, preserve resident ownership and dormant idle workers, and never replay the original prompt during automatic recovery.

**Non-Goals:** No new client UI or REST fields. All three clients share the daemon's corrected behavior. Investigating the origin of stale statuses is separate.

## Decisions

- The supervisor atomically records its running task IDs before StopAll in its data directory. Reconciliation consumes this snapshot only after an authoritative live-set query succeeds. This captures pending plain sessions as well as Hera sessions without relying on task status. Live Hera bindings on pending/in_progress/in_review tasks provide a crash fallback; legacy in_progress reconciliation remains. Residents, archived/complete tasks and ready_to_close tasks are excluded. Transcript idle classification keeps idle workers dormant even when their coordinator is recovering.
- Retain each client's Start sequence and the PID returned by its eventual successful RPC, including after a timeout. The client places the actual sequence assigned under its lock on the transient launch task; the recovery adapter captures it after Start returns. Predicting the next sequence before validation could accidentally claim a concurrent human launch. Recovery recognizes a live retry only when its actual sequence and PID match; a different launch remains superseded. An outstanding RPC defers retries without spending attempts, preserving its receipt. Note ownership requires no wire field and never guesses from liveness alone.
- Automatic recovery validates a nonempty conversation identity and backend resumability immediately before starting. A transient ResumeOnly flag crosses StartReq (protocol 14) and the supervisor repeats validation inside prepareResume, closing the daemon-to-supervisor availability race. Older or unknown supervisors are refused. Missing conversations fail; ordinary explicit user resume retains its existing fresh-start behavior.
- Recycle and successful live account-move restarts persist in_progress for ordinary tasks. The resident retains its separate manager-owned lifecycle: its DB guard refuses generic workflow status writes even when the shared recycle helper restarts it. Audit sibling restart callers and correct equivalent stale-status paths.

## Risks / trade-offs

- A hard supervisor crash cannot write a shutdown snapshot → eligible live Hera bindings and in_progress rows provide fallback; stale-status plain tasks require a graceful shutdown snapshot.
- A daemon restart loses in-memory RPC receipts → only confirmed owned starts get the retry exception; stable delivery IDs still deduplicate notes already queued.
- Transcript availability can change between daemon validation and supervisor launch → the supervisor enforces ResumeOnly at the actual launch seam.

## Migration plan

Ship daemon and supervisor together; restart both to activate supervisor snapshot persistence and daemon recovery. Protocol 14 adds the resume-only constraint (spawn surface 50, stream surface 18); automatic recovery refuses older supervisors until restarted. No database migration. Rollback ignores the new snapshot file.

The quality gate also requires the Go 1.26.11 toolchain patch: the previous 1.26.10 pin is affected by GO-2026-6859 in net/http. This is a toolchain maintenance update, with no new application feature. Run the standard gate through the existing host-side `script/iris-check pre-pr` entrypoint.

Host validation exposed two Git fixtures inheriting real user signing/hooks; isolate their HOME in their existing tests. The host check helper defaults to three concurrent Go packages, preserving explicit caller GOFLAGS, to follow the shared-host resource guidance without changing assertions, race detection, timeout or coverage requirements.

The dev-stack reaper integration tests also shared a mutable package timer with prior daemon goroutines. Make that test override per-daemon and retain the constant 10-second production default; this is a test-seam refactor, with no reaper behavior change.

## Open questions

What flipped coord-always-on-agent from in_progress to in_review while it was live between November 10 17:11 and November 12 05:18? The TUI stale-task check in app.go is suspected during a daemon bounce, but available logs cannot confirm it. This investigation is out of scope.
