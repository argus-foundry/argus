## ADDED Requirements

### Requirement: Hera panes apply the same keep-place gates

Both Hera panes SHALL hold a pending size-drift kick while the pane is scrolled up and SHALL restart the debounce dwell when the pane returns to the bottom, without issuing decision RPCs while held. The unsent-text gate SHALL apply to the bound task's session exactly as in the Tasks agent view. The Ctrl+Z zoom re-arm SHALL pass through the same gates.

#### Scenario: Scrolled-up Hera pane

- **WHEN** a bound pane's width crosses the margin while the pane is scrolled up
- **THEN** no kick evaluation is issued until the pane is at the bottom and the debounce dwell elapses

#### Scenario: Zoom while reading

- **WHEN** Ctrl+Z changes the pane width while it is scrolled up
- **THEN** the re-arm waits for the bottom and does not kick

#### Scenario: Draft in a Hera worker

- **WHEN** the bound worker's composer holds unsent text
- **THEN** the kick is deferred and retried through the existing retry interval
