## Decisions

- **Storage:** `lifecycle` is a third key in the existing `pr` namespace, written in the same `SetMetaBatchIfChanged` transaction as `state`/`url`, so one commit yields one `task.pr_changed`. A column was not needed: the value is refreshed by the poller and not an archive-surviving timestamp.
- **Derivation:** `gitutil.PRResult.Lifecycle()` — `Merged` gives merged; merged-closed without MERGED evidence gives closed; no PR or unknown gives unknown; everything else is open.
- **DTO projection:** `model.ResolvePRLifecycle(state, lifecycle)` prefers a verified stored value. A non-terminal `pr_state` (including draft) with no stored value projects `open`, since a non-terminal state can only come from an open PR. A merged-closed entry without lifecycle projects `unknown`.
- **Terminal refresh bound:** the poller skips a terminal entry only when its stored lifecycle is merged/closed/open-verified. Legacy terminal entries are re-fetched once; a successful fetch writes the lifecycle and the entry is skipped from then on. A failed fetch keeps the cached values and the entry is retried next cycle.
- **Event:** `model.PRChangedPayload` is a struct so a later change can add a link `source` key additively. `task.updated` with `pr` still fires from the meta write; `task.pr_changed` carries the PR facts for consumers that do not want to re-read.
- **Rollout:** the first poll after upgrade writes `lifecycle` for every cached non-terminal entry once, which emits one `task.pr_changed` per such task.
- **Deviation:** none from the ticket contract.
