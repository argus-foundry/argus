## MODIFIED Requirements

### Requirement: In-pane width repair status

The TUI SHALL display a small border indicator on the right side of a pane's top border. While a pane's repair is deferred, stopping, or restarting it SHALL say “Re-render pending” (deferred) or “Re-rendering…” (active stop or restart). When no repair is in flight but the pane's history is owed a re-render (setting `ui.auto_rerender_on_resize` off and drift at least `RerenderMargin`), it SHALL show the owed notice `width changed · <key> re-render`. Precedence SHALL be “Re-rendering…” over “Re-render pending” over the owed notice; at most one is drawn. Terminal content, including deferred user prompts, SHALL remain readable, and the indicator SHALL be truncated before it can overlap the left-side title. The indicator SHALL clear when repair resolves, restart fails, the session exits cleanly, or the pane rebinds, including inactive Hera panes; the owed notice SHALL additionally clear when the width returns inside the margin.

#### Scenario: Busy agent defers repair

- **WHEN** width repair is deferred because the agent is busy
- **THEN** its pane SHALL show the appropriate border status without covering terminal content

#### Scenario: Repair completes

- **WHEN** a restarted session is attached successfully or evaluation finds no repair necessary
- **THEN** the indicator SHALL disappear

#### Scenario: Owed notice yields to in-flight status

- **WHEN** a hotkey re-render is deferred while the owed notice is showing
- **THEN** the border shows “Re-render pending” and not the owed notice

#### Scenario: Owed notice draws on the top border's right side

- **WHEN** a SimulationScreen frame is drawn for a drifted pane with the setting off
- **THEN** the top border row contains the pane title on the left and `width changed · shift+home re-render` right-aligned
