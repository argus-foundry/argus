## ADDED Requirements

### Requirement: Auto re-render on resize setting

The UI configuration SHALL provide `auto_rerender_on_resize`, default false, stored as `ui.auto_rerender_on_resize`, with config.toml overriding the DB and an absent key retaining the default. It SHALL be readable live without restart, editable in Settings (local mode) and writable through the remote-settings path. With it true, the TUI's automatic size-drift kick SHALL behave exactly as before the `resize-without-restart` change. It SHALL govern the TUI only (the web and macOS clients keep their daemon-side kick). The setting SHALL also be readable and writable through `GET/PUT /api/settings` (`ui.auto_rerender_on_resize`) so a remote TUI can manage it. The effective value SHALL be logged at TUI start and on every live change.

#### Scenario: Default is off

- **WHEN** neither config.toml nor the DB sets the key
- **THEN** the effective value is false and widths change without restarts

#### Scenario: File overlay wins

- **WHEN** config.toml sets `auto_rerender_on_resize = true` and the DB stores false
- **THEN** the effective value is true

#### Scenario: Live toggle

- **WHEN** the user turns the setting on in Settings while a drifted pane shows the notice
- **THEN** the notice clears and the automatic kick evaluates without a TUI restart
