# Terminal Rendering

## ADDED Requirements

### Requirement: Reused emulators release scrollback memory

When a pane RIS-resets an emulator for reuse, the system SHALL zero the retained scrollback lines before clearing, so the memory is reclaimable rather than held at its peak.

#### Scenario: Reset frees retained lines

- **WHEN** an emulator holding scrollback is reset for reuse
- **THEN** the heap retained by its scrollback drops to near zero

### Requirement: Replay scrollback is bounded by a cell budget

After a replay build is fed, the system SHALL trim its scrollback oldest-first to a cell budget (default 1,000,000 cells) in addition to the line cap, and SHALL log the trim.

#### Scenario: Dense log is trimmed

- **WHEN** a replay feed produces more cells than the budget
- **THEN** only the newest lines within the budget remain and retained memory is at most ~budget x 112 B

#### Scenario: Trimmed build does not re-extend

- **WHEN** a build was trimmed and the user scrolls past its oldest line
- **THEN** no further log read is started and the scroll position clamps at the oldest retained line

### Requirement: Idle panes release replay emulators

A pane not drawn for 5 minutes SHALL release its spare slots and published replay emulator by shrinking them in place (never Close), SHALL skip release while a rebuild is in flight, and SHALL rebuild on return.

#### Scenario: Idle pane releases memory

- **WHEN** a pane has not been drawn for the idle threshold and no rebuild is in flight
- **THEN** its replay emulators retain no scrollback and the published replay emulator is cleared

#### Scenario: Active pane is untouched

- **WHEN** a pane was drawn within the idle threshold
- **THEN** nothing is released
