## ADDED Requirements

### Requirement: The size-drift kick preserves the user's place and input

The size-drift kick SHALL fire only when the agent is idle, not blocked on a prompt, the pane is at the bottom of its scrollback, and the composer holds no unsent text. When the pane is scrolled up or unsent text is present, the kick SHALL be deferred, SHALL keep its drift anchor, and SHALL re-arm through the existing debounce once the pane is back at the bottom and the draft is clear. Every kick and deferral SHALL be logged. With `ui.rerender_keep_place` off the behavior SHALL be unchanged from before this requirement.

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

#### Scenario: Draft signal order

- **WHEN** the composer-draft question is asked for a session
- **THEN** the signals are consulted in order: the draft-aware mod's report, then the rendered Claude composer (when it is found on screen), then the TUI-local typed-since-submit flag; a source that cannot answer (composer not found) falls through to the next and is never read as an empty composer

#### Scenario: Non-Claude session typed-since-submit

- **WHEN** a session with no live bridge and no recognisable Claude composer has received user typing since the last submit
- **THEN** the kick is deferred until a submit is sent or the deferral cap elapses

#### Scenario: Typed-since-submit deferral cap

- **WHEN** kicks at one width have been deferred for 10 minutes and the only draft signal is the typed-since-submit flag
- **THEN** the flag stops holding the kick, so a stale bit (for example text deleted with Backspace) cannot defer the re-wrap forever; the cap does not apply to a mod-reported or screen-confirmed draft

#### Scenario: Known limitation: other clients

- **WHEN** the user typed in the web app or another TUI, and no bridge or rendered-composer signal exists for the session
- **THEN** the typed-since-submit flag does not see it and the kick is not deferred for that text (web and macOS scroll-up gating and a web typed-since-submit signal are named follow-ups)

#### Scenario: Idle tick

- **WHEN** no kick is deferred for the task shown in the agent view
- **THEN** the periodic tick performs no session lookup, DB read, RPC or file access for the retry; real work happens only while a deferral is armed

#### Scenario: Setting off

- **WHEN** `ui.rerender_keep_place` is off
- **THEN** the kick ignores scroll position and draft state
