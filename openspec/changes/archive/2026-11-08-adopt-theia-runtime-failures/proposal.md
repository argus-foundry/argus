# Adopt live task runtime and failures

Use live and idle independently of workflow status for terminal readiness and busy icons. Reconcile runtime changes through task snapshots. Combine explicit task failure with Hera failure, render the reason in Info, and clear it from authoritative failure events.

## Non-Goals
No backend changes. Native follow-up `macos-task-runtime-and-failure-presentation` evaluates native runtime/failure UI. No implicit composer revival.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| Session activity | Value | Current measured live/idle state, independent of workflow | Task DTO |
| Task failure | Value | Explicit failed result and reason | Task DTO and failure event |
| Rail state | Policy | Combine attention, failure, runtime, workflow | Task forest |

Existing terminal policy and task forest remain the owners; no new actor is needed.
