## ADDED Requirements

### Requirement: macOS terminal keeps the user's place and draft across a resize kick

The macOS terminal SHALL send `scrolled_up` (SwiftTerm viewport above the bottom) and `composer_dirty` (the same typed-since-submit fold as the TUI and web, computed by a pure ArgusKit function over input actually sent to the agent, after Ctrl+Z sanitizing) with each resize, and SHALL surface the response's `deferred` value. While deferred it SHALL re-send the current size when scrolled back to the bottom and every 10 seconds, and SHALL stop when the deferral clears, a kick lands, or the terminal view is dismissed or its task changes.

#### Scenario: Scrolled up while the window resizes

- **WHEN** the pane is scrolled up and the window is resized past the margin
- **THEN** the daemon defers the kick and the client retries until the pane is at the bottom

#### Scenario: Typed fold

- **WHEN** input is sent as "fix" then Enter
- **THEN** the flag is set after "fix" and cleared after Enter

#### Scenario: View dismissed

- **WHEN** the terminal view is dismissed while a retry is pending
- **THEN** no further resize request is made
