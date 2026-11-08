## ADDED Requirements

### Requirement: The size-drift kick preserves the user's place and input

The size-drift kick SHALL fire only when the agent is idle, not blocked on a prompt, the pane is at the bottom of its scrollback, and the composer holds no unsent text. When the pane is scrolled up or unsent text is present, the kick SHALL be deferred, SHALL keep its drift anchor, and SHALL re-arm through the existing debounce once the pane is back at the bottom and the draft is clear. Every kick and deferral SHALL be logged. With `bridge.keep_place` off the behavior SHALL be unchanged from before this requirement.

#### Scenario: Idle at the bottom, empty composer

- **WHEN** the width drifts past the margin, the agent is idle, the pane is at the bottom and the composer is empty
- **THEN** the kick fires after the existing debounce

#### Scenario: Scrolled up while reading

- **WHEN** a kick becomes eligible while the pane is scrolled up
- **THEN** no kick fires and a deferral is logged

#### Scenario: Back at the bottom

- **WHEN** the user scrolls back to the bottom after a scroll deferral
- **THEN** the kick fires after the debounce

#### Scenario: Unsent text defers the kick

- **WHEN** a kick becomes eligible while the composer holds unsent text
- **THEN** no kick fires until the draft clears

#### Scenario: Non-Claude session typed-since-submit

- **WHEN** a session with no live bridge has received user typing since the last submit
- **THEN** the kick is deferred until a submit is sent

#### Scenario: Setting off

- **WHEN** `bridge.keep_place` is off
- **THEN** the kick ignores scroll position and draft state
