## ADDED Requirements

### Requirement: Recovery ownership across background transitions

Hera reclaim SHALL establish that a role nuke reserved the task before reclaiming its resources. A pending graduation SHALL prevent a nuke of its associated role. PWA background refresh SHALL preserve an active terminal recovery deadline, and reauthentication SHALL preserve the selected tab.

#### Scenario: Legacy deletion preserved another binding
- **WHEN** a legacy role nuke predates another role's binding end or a later binding
- **THEN** ordinary rebind or detach does not make the legacy nuke a cleanup claim.

#### Scenario: A hidden role has pending graduation
- **WHEN** the latest ended binding identifies a task with a pending graduation
- **THEN** a nuke leaves the role and reclaim state unchanged.

#### Scenario: Resident cleanup lacks retirement confirmation
- **WHEN** resident cleanup has no current reclaim claim or the session owner cannot confirm retirement
- **THEN** it preserves the worktree and defers eligible cleanup until retirement is confirmed.

#### Scenario: A background refresh observes replacement is live
- **WHEN** ordinary PWA refresh or reconnect sees a replacement while terminal recovery is active
- **THEN** attachment remains subject to the existing recovery deadline.

#### Scenario: Logging out from another tab
- **WHEN** the PWA user logs out and signs in again on the same page
- **THEN** the selected tab remains the only tab displayed.
