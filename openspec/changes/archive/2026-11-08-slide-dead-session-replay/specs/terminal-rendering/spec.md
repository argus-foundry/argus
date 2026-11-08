# Terminal Rendering

## ADDED Requirements

### Requirement: Dead-session replay slides within its final frame

A dead session's replay SHALL be bounded to the log offset of its final alt-screen exit (the whole log's last `CSI ?1049l`; the log size when there is none or it sits at offset 0), computed once per (session, log size) and cached. Within `[0, bound)` the replay window SHALL slide back and forward exactly like a live session's, so every byte up to the final frame stays reachable at bounded memory, and the post-exit teardown output SHALL never be shown.

#### Scenario: Scrolling a dead session through a log larger than the budget

- **WHEN** the user scrolls a dead session from its last frame to the very first line and back
- **THEN** every step shows contiguous content, the cursor line is stable across window seams, and the resume hint printed after the alt-screen exit is never displayed

#### Scenario: The bound is found against the whole log

- **WHEN** the final alt-screen exit lies outside the most recent tail read, or spans a scan-block boundary
- **THEN** the replay still ends at that exit

#### Scenario: A grown log is rescanned

- **WHEN** the session's log grows (the session was restarted)
- **THEN** the bound is recomputed rather than served from the cache
