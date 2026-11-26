## ADDED Requirements

### Requirement: Nuke and terminal admission

An ended last binding from a committed role nuke SHALL fence generic operations before archival. Nuke refusals SHALL be visible and keep any unsettled roles reachable. Terminal recovery SHALL remain bounded until snapshot rendering and stream attachment finish.

#### Scenario: Role nuke commits while archive is deferred
- **WHEN** another lifecycle operation temporarily prevents archive
- **THEN** generic operations preserve the unarchived task and periodic recovery verifies its unsettled nuke under lock.

#### Scenario: A cascade cannot mutate a role
- **WHEN** a role mutation is refused
- **THEN** the operator is told and the orchestrator remains reachable.

#### Scenario: Standalone cleanup retained the archived task
- **WHEN** an ordinary archive or metadata update follows completed reclaim
- **THEN** it cannot re-arm the already settled binding.

#### Scenario: Recovery loses its view or cannot complete rendering and attachment
- **WHEN** the view closes, its snapshot rendering stalls, or the replacement SSE stream refuses authentication or attachment
- **THEN** outdated work cannot open another stream and retries remain within the original recovery deadline.
