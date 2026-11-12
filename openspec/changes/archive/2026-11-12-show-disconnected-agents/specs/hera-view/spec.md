## ADDED Requirements

### Requirement: Rail shows a disconnected glyph for a role whose session is not running

The Hera rail and plan-view nodes SHALL render a dim grey hollow circle `○` for a live-bound role whose bound task is in_progress or in_review and whose session is absent from the daemon's alive set for 2 consecutive ticks. The glyph SHALL rank below needs-input and the active spinner and above accepted, ready_to_close, failed, done, idle and live. The signal SHALL be suppressed whenever the daemon session list could not be read that tick.

#### Scenario: Dead agent behind a stale ready_to_close

- **WHEN** a worker is stamped ready_to_close but its session is gone from the daemon alive set for 2 ticks
- **THEN** its rail row shows `○`, not the review glyph

#### Scenario: Blocked prompt outranks disconnected

- **WHEN** a role shows needs-input
- **THEN** the needs-input glyph is shown, not `○`

#### Scenario: Unreadable session list never marks anyone disconnected

- **WHEN** the session-list RPC fails on a tick
- **THEN** no role is newly marked disconnected

#### Scenario: Completed task is never disconnected

- **WHEN** a role's task status is complete and its session is gone
- **THEN** the accepted glyph is shown, not `○`
