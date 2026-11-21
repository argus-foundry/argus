## ADDED Requirements

### Requirement: Daemon nudges coordinators about archivable roles

When hera is enabled, the daemon SHALL run a watcher that tells a coordinator, through its normal hera inbox, which of its worker and freelance roles have become archivable.

A role is archivable when a dry-run evaluation identical to `hera_archive(dry_run=true)` for that single role reports it archivable on either the merged or the handed-off path. That evaluation reuses the existing verdict, including the requirement that a live session be idle and not parked at a prompt. Any refusal, including a lookup error, SHALL produce no nudge for that role.

Triggers:

- The watcher SHALL be poked when the PR poller commits a changed PR state, when a role reports `hera_status(done)`, and when `hera_accept` runs.
- After a debounce it SHALL sweep.
- It SHALL also sweep periodically at a low frequency.

Batching and dedup:

- All archivable roles under one coordinator found in a sweep SHALL go out as ONE message.
- At most one nudge SHALL be sent per coordinator within a minimum gap.
- A role SHALL NOT be nudged again unless its archive kind, task status or PR state changed since the last nudge, or a long cooldown has passed. That record SHALL survive a daemon restart.

Exclusions and delivery:

- The watcher SHALL never nudge about coordinators, or about pinned roles, resident tasks or standing-team members.
- The message SHALL be a self-addressed `[argus]` system notice to the role's orchestrator coordinator, delivered through the same idle-gated doorbell as other hera messages. A coordinator without a live binding SHALL receive nothing, and nothing is recorded.
- The watcher SHALL NOT archive, stop, or change any role or task.
- It SHALL log its sweeps, nudges and skips with uxlog.

#### Scenario: Handed-off worker triggers one batched nudge

- **WHEN** two workers under the same coordinator are idle with open, green, pushed PRs and `in_review` tasks, and the watcher sweeps
- **THEN** the coordinator receives exactly one hera message whose body starts with `[argus] ` and lists both role names with their archive kind
- **AND** neither role nor task is archived by the daemon

#### Scenario: Busy or refused roles are not nudged

- **WHEN** a worker's verdict refuses (CI failing, session busy, task `in_progress`, or a lookup error)
- **THEN** no nudge mentions that role

#### Scenario: No repeat nudge for an unchanged role

- **WHEN** a role was nudged and its archive kind, task status and PR state are unchanged, and the cooldown has not passed
- **THEN** later sweeps (including after a daemon restart) send no nudge about it

#### Scenario: Changed state re-nudges

- **WHEN** a role nudged as handed off later becomes archivable as merged
- **THEN** the next eligible sweep nudges the coordinator about it again

#### Scenario: Excluded roles are never nudged

- **WHEN** a role is a coordinator, is pinned, or is bound to a resident or standing-team task
- **THEN** the watcher never lists it in a nudge

#### Scenario: Coordinator gap batches later finds

- **WHEN** a role becomes archivable within the minimum gap after a nudge to the same coordinator
- **THEN** no second message is sent during the gap, and the role is included in the next nudge after the gap

#### Scenario: Poke runs a sweep after the debounce

- **WHEN** a worker reports `hera_status(done)` and is idle with a green open PR when the debounce elapses
- **THEN** the coordinator is nudged without waiting for the periodic sweep
