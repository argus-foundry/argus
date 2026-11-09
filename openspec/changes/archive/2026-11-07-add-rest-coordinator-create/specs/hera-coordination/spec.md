## ADDED Requirements

### Requirement: Coordinator creation is idempotent per request_id

The daemon SHALL persist a receipt for each coordinator-creation `request_id` and canonical body before spawning. A completed identical retry SHALL return the same identities without spawning; an in-flight retry or a changed body SHALL return 409; the receipt SHALL survive a daemon restart; a failed spawn SHALL release the receipt so the id can retry.

#### Scenario: Replay after restart
- **WHEN** an identical request is retried after the daemon restarts
- **THEN** the original identities are returned and no second team exists

### Requirement: Coordinator creation event

After a coordinator is created over REST the daemon SHALL emit `hera.orchestrator_created` with `{orchestrator_id, role_id, task_id}`, and SHALL NOT emit it on failure.

#### Scenario: Event on success only
- **WHEN** creation succeeds
- **THEN** exactly one event is emitted
