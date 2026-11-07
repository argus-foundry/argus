## ADDED Requirements

### Requirement: PR lifecycle persistence

The poller SHALL persist `lifecycle` (`open`, `merged`, `closed` or `unknown`) under namespace `pr` alongside `state` and `url`, in the same transaction. `merged` SHALL require GitHub's raw MERGED state; a CLOSED PR SHALL yield `closed`; no PR SHALL yield `unknown`. A terminal cached `state` without a verified `lifecycle` (`merged`, `closed` or `open`) SHALL stay eligible for polling until a successful fetch stores one, after which it is skipped. A failed fetch SHALL leave the stored lifecycle untouched.

#### Scenario: Closed is not merged

- **WHEN** GitHub reports CLOSED for a task's PR
- **THEN** `lifecycle` is stored as `closed` and no merge notification fires

#### Scenario: Legacy terminal entry is refreshed once

- **WHEN** a task's cached `state` is `merged-closed` with no `lifecycle`
- **THEN** the next poll fetches it, stores the lifecycle, and later polls skip it

### Requirement: PR change event

After a PR cache commit that changed a stored value, the poller SHALL publish `task.pr_changed` with `{"url","state","lifecycle"}`. An unchanged result SHALL publish nothing.

#### Scenario: Changed result publishes once

- **WHEN** a poll changes a task's cached PR values
- **THEN** exactly one `task.pr_changed` is published, and a repeat poll with the same result publishes none
