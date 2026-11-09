## ADDED Requirements

### Requirement: Live bind replays each log segment at its authored size

When the live emulator is built or rebuilt from the session log (fresh bind, rebind, or ring-wrap rebuild) and a width index exists for the log, the replay SHALL emulate each segment at the PTY size it was authored for, using the offset of the first replayed byte within the log, and return the emulator to its build size afterwards. When the history comes from the ring-only fallback, which has no log offsets, or no index exists, the replay SHALL be a single-size feed.

#### Scenario: Recycled session resized after spawn
- **WHEN** a session was spawned at 140x69, resized to the 88x48 pane near the end of its log, and a pane binds to it
- **THEN** the pane shows the final message above exactly one footer, with no overlaid stale footer glyphs

#### Scenario: Ring-only history
- **WHEN** no session log exists and the history comes from the ring buffer
- **THEN** no width index is applied and the history is fed as one write
