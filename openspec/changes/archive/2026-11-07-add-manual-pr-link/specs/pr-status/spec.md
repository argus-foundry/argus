## ADDED Requirements

### Requirement: Manual PR association

The system SHALL store a manually linked PR URL per task in `task_pr_links`, separate from the derived `pr` cache, surviving archive and daemon restart and removed only by clearing it or deleting the task. The branch poller SHALL NOT poll or write cache for a linked task, and its write SHALL be refused inside the write transaction. A manual pass SHALL verify the linked PR by URL through `gh`, storing `state`, `url` (the linked URL) and `lifecycle`; a lookup error SHALL keep the association and last verified cache. Setting or clearing an existing link SHALL delete the task's `pr` cache.

#### Scenario: Link survives conflicting branch result

- **WHEN** a linked task shares a branch whose lookup returns a different PR
- **THEN** the linked task's cache and association still refer to the linked PR

#### Scenario: Clearing restores discovery

- **WHEN** the link is cleared
- **THEN** the next branch poll caches the branch-derived PR


The manual pass SHALL skip missing and archived tasks and use the branch poller's shared dormancy and terminal-verification eligibility. A linked cache SHALL be trusted only when its stored URL matches the manual link. A mismatched terminal cache SHALL receive the bounded immediate terminal verification attempt, then normal dormancy retries on failure; projection SHALL report the linked URL with unknown state/lifecycle and null checked_at until verified. Refused automatic writes SHALL emit neither a PR change event nor either merge notice. Task pruning SHALL remove associations in the same transaction as the task rows. Clearing an absent link SHALL preserve automatic cache and emit no mutation events.

#### Scenario: Link changes during a branch fetch

- **WHEN** a branch fetch returns merged after a different manual link is installed
- **THEN** the refused result sends neither merge notice nor PR change event

#### Scenario: Prune removes durable intent atomically

- **WHEN** either prune entry point deletes a linked task
- **THEN** the task and link are deleted together, and a failed task deletion rolls back link removal
- **AND** a legacy orphan link is skipped without fetching or writing cache

#### Scenario: Manual eligibility follows task activity

- **WHEN** a linked task is archived, or its dormant unknown cache is not due this cycle
- **THEN** the manual pass skips its URL lookup

#### Scenario: Re-upgrade rejects a foreign terminal cache

- **WHEN** an older binary has cached a merged branch PR different from the manual link
- **THEN** GET projects the linked URL as unknown and the manual pass re-verifies it with bounded retries
