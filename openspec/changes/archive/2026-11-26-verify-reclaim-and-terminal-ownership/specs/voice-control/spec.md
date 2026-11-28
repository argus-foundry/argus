## ADDED Requirements

### Requirement: Current ownership before cleanup or live attachment

Cleanup SHALL verify that a task still owns its local worktree or branch, and stacked cleanup SHALL honor a resident cleanup claim. Terminal clients SHALL keep ownerless recorded history separate from a replacement process. PWA mutation controls SHALL belong to one detail visit.

#### Scenario: A worktree is reused
- **WHEN** deferred cleanup encounters resources now owned by a different task
- **THEN** the local replacement is preserved, regardless of whether the earlier task is still present.

#### Scenario: A resident cleanup also belongs to a merged stack
- **WHEN** another cleanup candidate includes a branch with a resident cleanup claim
- **THEN** that candidate leaves the branch to the resident cleanup lease.

#### Scenario: Recorded output and a newly live process
- **WHEN** a terminal loads recorded output without a session PID and subsequently sees a live process
- **THEN** it obtains that process's snapshot before displaying live bytes.

#### Scenario: A blocked action outlives a detail visit
- **WHEN** the user leaves a detail while Resume or Delete is pending
- **THEN** the next visit starts with independent action controls, and its model can still refresh authoritative task names.
