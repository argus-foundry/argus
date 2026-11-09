## ADDED Requirements

### Requirement: A width change resizes the agent without restarting it

With `ui.auto_rerender_on_resize` off (the default), when the agent view's pane width changes (window resize, Ctrl+Z fullscreen toggle, task switch at a different width) the TUI SHALL resize the PTY (SIGWINCH) and SHALL NOT stop, kill, or resume the agent. The live region is repainted by the agent itself. Existing scrollback SHALL keep the width it was authored at and SHALL be displayed per authored-width segment.

#### Scenario: Resize does not restart

- **WHEN** the pane width changes by 40 columns with the setting off
- **THEN** the PTY is resized, the session keeps its PID and session ID, and no kick or deferral is logged

#### Scenario: Fullscreen toggle does not restart

- **WHEN** the user toggles Ctrl+Z twice with the setting off
- **THEN** no kill+resume happens and the live region is repainted by the agent

### Requirement: Owed re-render notice on the pane frame

When the setting is off and the pane's width differs from the session's authored width (the existing drift predicate: `|paneCols - initCols|` or `|paneCols - committedCols|` at least `RerenderMargin`), the agent view's frame SHALL show a notice on the right side of its top border, mirroring the left-side title, reading `width changed · <key> re-render` where `<key>` is the live binding of `agent.rerender` (or a palette pointer when unbound). The notice SHALL clear after a re-render completes, when the width returns inside the margin, and on rebind. Transitions SHALL be logged once each, not per frame.

#### Scenario: Notice appears on drift

- **WHEN** the pane is narrowed by at least the margin
- **THEN** the top border's right side shows `width changed · ctrl+n re-render`

#### Scenario: Notice clears when width returns

- **WHEN** the pane returns to within the margin of the authored width
- **THEN** the notice disappears and a `cleared` line with reason `width-returned` is logged

#### Scenario: Notice never covers content

- **WHEN** the pane is narrower than the notice plus the title
- **THEN** the notice is truncated or omitted and the title and terminal content remain intact

### Requirement: Owed notice is clickable

A left click on the owed re-render notice SHALL run the same on-demand re-render as the hotkey, subject to the same safety gates. A click anywhere else on the border or pane SHALL keep its existing behaviour, and the click SHALL NOT be forwarded to the PTY.

#### Scenario: Click re-renders

- **WHEN** the notice is showing, the agent is idle and the user clicks the notice
- **THEN** the session is re-rendered at the pane width and the notice clears

#### Scenario: Click elsewhere is unchanged

- **WHEN** the user clicks the pane body while the notice is showing
- **THEN** no re-render runs and click handling is as before

### Requirement: On-demand re-render hotkey

The `agent.rerender` action SHALL run the size-drift kick (stop and resume at the current pane width, with keep-place and draft restore) for the focused agent. It SHALL bypass the debounce but SHALL NOT bypass the safety gates: a busy, prompt-blocked, scrolled-up, or draft-holding agent SHALL defer with a status-bar explanation and the existing "Re-render pending" status. With no drift it SHALL be a no-op with a status-bar notice. Each invocation SHALL be logged with its outcome.

#### Scenario: Hotkey re-renders

- **WHEN** the notice is showing, the agent is idle and the user presses the bound key
- **THEN** the session is stopped and resumed at the pane width and the notice clears

#### Scenario: Hotkey respects the prompt gate

- **WHEN** the agent is blocked on an AskUserQuestion prompt and the user presses the key
- **THEN** the agent is not stopped and the status bar says the re-render is pending

#### Scenario: Hotkey with no drift

- **WHEN** the width is inside the margin and the user presses the key
- **THEN** nothing restarts and the status bar says history already matches this width

### Requirement: Setting restores the automatic size-drift kick

With `ui.auto_rerender_on_resize` on, the automatic size-drift kick SHALL behave exactly as before this change (entry-time evaluation, debounce, retry, all gates), the owed notice SHALL NOT be shown, and the hotkey SHALL still work.

#### Scenario: Setting on restores the kick

- **WHEN** the setting is on and the width drifts past the margin with an idle agent
- **THEN** the kick fires after the existing debounce as before

## MODIFIED Requirements

### Requirement: The size-drift kick preserves the user's place and input

Every size-drift kick, whether automatic (setting on) or requested with the re-render hotkey, SHALL fire only when the agent is idle, not blocked on a prompt, the pane is at the bottom of its scrollback, and the composer holds no unsent text. When the pane is scrolled up or unsent text is present, the kick SHALL be deferred, SHALL keep its drift anchor, and SHALL re-arm through the existing debounce (automatic) or retry (hotkey) once the pane is back at the bottom and the draft is clear. Every kick and deferral SHALL be logged. With `ui.rerender_keep_place` off the behavior SHALL be unchanged from before this requirement.

#### Scenario: Idle at the bottom, empty composer

- **WHEN** the width drifts past the margin, the agent is idle, the pane is at the bottom and the composer is empty, and a kick is requested (automatic with the setting on, or the hotkey)
- **THEN** the kick fires (after the existing debounce when automatic)

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
