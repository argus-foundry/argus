## ADDED Requirements

### Requirement: Scroll replay emulates each log segment at its authored width

When a width index exists for the session log, the replay SHALL feed the log in segments and resize the emulator at each recorded offset so each segment is emulated at the PTY size it was authored for, start a window that begins mid-log at the size in force at its start, and return the emulator to its build size after the last segment so painting and anchoring are unchanged. Without an index, or when the log is read from the ring-only fallback, the replay SHALL behave as a single-width feed.

#### Scenario: Relative redraw across a width change
- **WHEN** a segment authored at 20 columns redraws its two-row UI with a relative cursor-up of 2, and the PTY was later widened to 40
- **THEN** the replay shows the redrawn UI wrapped at 20 columns and the content above it intact

#### Scenario: No index
- **WHEN** the session has no width index
- **THEN** the log is emulated at one width as before

#### Scenario: Window starts mid-log
- **WHEN** a sliding window begins after a recorded size change
- **THEN** the window is fed starting at the size in force at its first byte
