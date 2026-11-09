## ADDED Requirements

### Requirement: Scroll replay of a live session emulates at its initial width

The scroll-mode replay of a live session SHALL build its emulator at the widest of the pane width, the session's current PTY width and the session's initial PTY width (when the handle reports one), then clip to the pane. The live PTY width alone SHALL NOT be used, because it follows resizes while the committed history stays baked at the width the session started at.

#### Scenario: Shrunk pane does not fragment wide history
- **WHEN** a live session started at 282 columns has its PTY shrunk to 142 by a Ctrl+Z and the user is scrolled up
- **THEN** the replay emulator is built at 282 columns and painted clipped to the pane, with no left-edge word fragments or right-edge letter column

#### Scenario: Growing past the initial width
- **WHEN** the live PTY is wider than the initial width
- **THEN** the replay emulator is built at the live PTY width

#### Scenario: Handle without an initial size
- **WHEN** the session handle reports no initial PTY size
- **THEN** the replay emulator width is the larger of the pane and the live PTY width, as before
