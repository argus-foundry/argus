## ADDED Requirements

### Requirement: Hera panes resize without restarting and show the owed re-render notice

With `ui.auto_rerender_on_resize` off (the default), a width change of the Hera coordinator pane or agent pane (rail rebind at a new width, Ctrl+Z fullscreen toggle, window resize) SHALL resize the PTY only and SHALL NOT stop or resume the bound agent. Each pane whose bound session's authored width differs from the pane width by at least `RerenderMargin` SHALL show the owed re-render notice on the right side of its top border, per the agent-view requirement, evaluated from the width set in `Draw` after `SetRect`. The `agent.rerender` hotkey SHALL act on the focused pane's task. The kick debounce, zoom dwell and retry backoff SHALL apply only while the setting is on.

#### Scenario: Rail navigation does not restart

- **WHEN** the user moves the rail selection to a task whose session was authored at a different width
- **THEN** the pane binds, resizes the PTY, shows the notice, and no kick is evaluated

#### Scenario: Hotkey in the coordinator pane

- **WHEN** the coordinator pane is focused, shows the notice, and the user presses the bound key
- **THEN** the coordinator's task is re-rendered at the pane width and the notice clears

#### Scenario: Unfocused pane keeps its own notice

- **WHEN** the agent pane shows the notice and the coordinator pane has no drift
- **THEN** only the agent pane's border carries the notice

## MODIFIED Requirements

### Requirement: Zoom toggle kick dwell and bounded retry backoff

While `ui.auto_rerender_on_resize` is on, a Ctrl+Z fullscreen toggle that moves a bound Hera pane's width SHALL re-arm the size-drift kick evaluation behind a 100ms dwell (not the 300ms rail-traversal dwell) and SHALL reset the busy-deferral retry backoff to its first interval. The busy-deferral retry interval SHALL start at 10s, double on each consecutive deferral and cap at 15s. A rebind SHALL still reset the backoff. A busy or prompt-blocked agent SHALL still never be killed to repair width. With the setting off the toggle only resizes the PTY and updates the owed notice.

#### Scenario: Zoom evaluates promptly
- **WHEN** the setting is on, the user toggles fullscreen and the pane's width changes
- **THEN** the kick is evaluated once 100ms have elapsed, not 300ms

#### Scenario: Zoom restarts a long backoff
- **WHEN** the setting is on, a coordinator has been deferring busy until its retry interval reached the cap and the user toggles fullscreen
- **THEN** the next deferral's retry is armed at 10s

#### Scenario: Backoff is capped
- **WHEN** a kick is deferred repeatedly with the setting on
- **THEN** successive retry delays are 10s, 15s, 15s and so on

#### Scenario: Zoom with the setting off
- **WHEN** the setting is off and the user toggles fullscreen
- **THEN** no kick evaluation is scheduled and the notice reflects the new width

### Requirement: Hera panes apply the same keep-place gates

Both Hera panes SHALL hold a pending size-drift kick (automatic with the setting on, or requested with the hotkey) while the pane is scrolled up and SHALL restart the debounce dwell when the pane returns to the bottom, without issuing decision RPCs while held. The unsent-text gate SHALL apply to the bound task's session exactly as in the Tasks agent view. The Ctrl+Z zoom re-arm (setting on) SHALL pass through the same gates.

#### Scenario: Scrolled-up Hera pane

- **WHEN** a bound pane's width crosses the margin while the pane is scrolled up and a kick is pending
- **THEN** no kick evaluation is issued until the pane is at the bottom and the debounce dwell elapses

#### Scenario: Zoom while reading

- **WHEN** Ctrl+Z changes the pane width while it is scrolled up with the setting on
- **THEN** the re-arm waits for the bottom and does not kick

#### Scenario: Draft in a Hera worker

- **WHEN** the bound worker's composer holds unsent text
- **THEN** the kick is deferred and retried through the existing retry interval
