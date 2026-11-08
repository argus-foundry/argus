# Terminal Rendering

## ADDED Requirements

### Requirement: Reused emulators release scrollback memory

When a pane RIS-resets an emulator for reuse, the system SHALL zero the retained scrollback lines before clearing, so the memory is reclaimable rather than held at its peak.

#### Scenario: Reset frees retained lines

- **WHEN** an emulator holding scrollback is reset for reuse
- **THEN** the heap retained by its scrollback drops to near zero

### Requirement: Replay scrollback is a bounded sliding window

A replay emulator SHALL hold at most a cell budget of scrollback (default 1,000,000 cells), and the budget SHALL be a window over the session log, not a floor: scrolling to the top of a budget-limited window SHALL rebuild it further back in the log (dropping the newest end), and scrolling to the bottom of a back-shifted window SHALL slide it forward again, so every byte of history stays reachable at bounded memory. A slide SHALL reuse the existing single-flight async rebuild and generation guard, SHALL read the log only on an actual slide, SHALL keep the same content line under the viewport top (preserving any scroll made during the rebuild), and SHALL apply ESC-boundary alignment, scroll-region clamping and OSC filtering to the window. Each trim, slide and release SHALL be logged.

#### Scenario: Scrolling through a log larger than the budget

- **WHEN** the user scrolls from the live bottom to the very first line of a log several times the budget, and back
- **THEN** every step shows contiguous content, the cursor line is stable across each window seam, the first and last lines are reachable, and retained memory stays within the budget throughout

#### Scenario: Window slides only when needed

- **WHEN** the viewport is well inside the current window
- **THEN** no log read or rebuild occurs

#### Scenario: Top of the log is a clamp

- **WHEN** a window already starts at the log's first byte and the user scrolls past its top
- **THEN** the scroll position clamps and no rebuild runs

### Requirement: Idle panes release replay emulators

A pane not drawn for 5 minutes SHALL release its spare slots and published replay emulator, and reset its live emulator, by shrinking them in place (never Close). The replay release SHALL be skipped while a rebuild is in flight. On return the live emulator SHALL be rebuilt from the log before the first painted frame, so no blank or torn frame is shown.

#### Scenario: Idle pane releases memory

- **WHEN** a pane has not been drawn for the idle threshold and no rebuild is in flight
- **THEN** its replay emulators retain no scrollback and the published replay emulator is cleared

#### Scenario: Active pane is untouched

- **WHEN** a pane was drawn within the idle threshold
- **THEN** nothing is released
