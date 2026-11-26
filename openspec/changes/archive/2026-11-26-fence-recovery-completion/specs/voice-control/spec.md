## ADDED Requirements

### Requirement: Recovery owns its operation through completion

PWA terminal recovery SHALL act only on the current attachment and SHALL enforce its deadline while waiting for a snapshot. Hera reclaim SHALL recover contended operations without bypassing resource ownership.

#### Scenario: An old terminal recovery loses ownership
- **WHEN** a newer attachment replaces a recovering terminal view
- **THEN** the old operation cannot mutate or restart the newer view.

#### Scenario: Recovery snapshot stalls
- **WHEN** snapshot response or rendering does not finish before the recovery deadline
- **THEN** the recovery is canceled and its queued input is settled.

#### Scenario: Initial nuke archive contends or bulk delete races
- **WHEN** nuke has ended the last binding and archive could not acquire the owner lock
- **THEN** the daemon retries archival and scoped stop, while generic bulk deletion skips any task that reclaim owns before resource side effects.

#### Scenario: Standalone nuke finishes without a worktree
- **WHEN** the standalone owner is retired and branch cleanup has finished
- **THEN** ownership is released while the archived task stays available to normal task actions.

#### Scenario: Retirement receipt lookup fails
- **WHEN** owner retirement is confirmed but the receipt lookup fails temporarily
- **THEN** reclaim retries the lookup and does not treat the transport failure as record expiration.
