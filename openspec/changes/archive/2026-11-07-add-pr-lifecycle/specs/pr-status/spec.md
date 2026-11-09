## ADDED Requirements

### Requirement: PR lifecycle persistence

The poller SHALL persist `lifecycle` (`open`, `merged`, `closed` or `unknown`) under namespace `pr` alongside `state` and `url`, in the same transaction. `merged` SHALL require GitHub's raw MERGED state; a CLOSED PR SHALL yield `closed`; no PR SHALL yield `unknown`. A terminal cached `state` without a verified `lifecycle` (`merged` or `closed`) SHALL receive one immediate verification attempt per task per daemon process. Later attempts SHALL follow the task's normal dormancy cadence if fetching fails or the response omits the branch. A successful fetch that stores `merged` or `closed` SHALL exclude the task from future polls. An authoritative no-PR response SHALL store `none`/`unknown` and resume ordinary dormancy polling. A terminal state paired with `open` is unverified and SHALL project `unknown` until refreshed. Refreshing an already terminal cache SHALL persist lifecycle and publish any changed-cache event without repeating either merge notification. A failed fetch SHALL leave the stored lifecycle untouched.

#### Scenario: Closed is not merged

- **WHEN** GitHub reports CLOSED for a task's PR
- **THEN** `lifecycle` is stored as `closed` and no merge notification fires

#### Scenario: Legacy terminal entry is refreshed once

- **WHEN** a task's cached `state` is `merged-closed` with no verified terminal `lifecycle`, including a stale `open` from a rolled-back binary
- **THEN** the next poll makes one immediate verification attempt regardless of dormancy and, on success, stores the lifecycle without repeating merge notices and skips later polls

#### Scenario: Unsuccessful terminal verification uses dormancy retries

- **WHEN** the immediate verification fetch fails or returns no result for the branch
- **THEN** the cached triplet is unchanged and later attempts follow dormancy cadence until verification succeeds
- **AND** a daemon restart permits one new immediate attempt, while verified terminal entries remain skipped

### Requirement: PR change event

After a PR cache commit that changed a stored value, outside the database mutex, the poller SHALL publish `task.pr_changed` with `{"url","state","lifecycle"}`. An unchanged result SHALL publish nothing.

#### Scenario: Changed result publishes once

- **WHEN** a poll changes a task's cached PR values
- **THEN** exactly one `task.pr_changed` is published, and a repeat poll with the same result publishes none
