## REMOVED Requirements

### Requirement: Rail shows a disconnected glyph for a role whose session is not running

**Reason**: The hollow `○` outranked accepted/ready_to_close/done and hid real state. Replaced by greying the normal icon.

**Migration**: See "Rail greys the status icon of a detached role".

## ADDED Requirements

### Requirement: Rail greys the status icon of a detached role

The Hera rail, plan-view nodes and details-pane roster SHALL render a detached role's normal status icon (the one its other signals would select, with the same precedence as a non-detached role) in the dimmed grey style instead of a disconnected `○`. A role is detached when its live-bound task is in_progress or in_review, not archived, and its session is absent from the daemon's alive set for 2 consecutive ticks; the signal SHALL be suppressed whenever the session list could not be read that tick, and a complete task SHALL never be detached. The grey treatment SHALL apply to every detached role regardless of state. An animated working spinner on a detached role SHALL render as a single static glyph. The details-pane roster status label SHALL show the role's normal state text followed by ` · detached` (e.g. `ready · detached`, `accepted · detached`).

#### Scenario: Finished worker with an open PR keeps its review icon

- **WHEN** a worker is stamped ready_to_close and its session is gone from the daemon alive set for 2 ticks
- **THEN** its rail row shows the ready_to_close icon rendered grey, not `○`

#### Scenario: Detached working role does not animate

- **WHEN** a detached role would otherwise show the working spinner
- **THEN** a static grey spinner glyph is shown and it does not change between frames

#### Scenario: Roster label names state and detached

- **WHEN** a detached role's state is ready_to_close
- **THEN** its roster status label reads `ready · detached`

#### Scenario: Unreadable session list never marks anyone detached

- **WHEN** the session-list RPC fails on a tick
- **THEN** no role is newly rendered grey as detached

#### Scenario: Completed task is never detached

- **WHEN** a role's task status is complete and its session is gone
- **THEN** the accepted icon is shown in its normal colour
