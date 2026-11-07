## ADDED Requirements

### Requirement: Manual PR association

The system SHALL store a manually linked PR URL per task in `task_pr_links`, separate from the derived `pr` cache, surviving archive and daemon restart and removed only by clearing it or deleting the task. The branch poller SHALL NOT poll or write cache for a linked task, and its write SHALL be refused inside the write transaction. A manual pass SHALL verify the linked PR by URL through `gh`, storing `state`, `url` (the linked URL) and `lifecycle`; a lookup error SHALL keep the association and last verified cache. Setting or clearing a link SHALL delete the task's `pr` cache.

#### Scenario: Link survives conflicting branch result

- **WHEN** a linked task shares a branch whose lookup returns a different PR
- **THEN** the linked task's cache and association still refer to the linked PR

#### Scenario: Clearing restores discovery

- **WHEN** the link is cleared
- **THEN** the next branch poll caches the branch-derived PR
