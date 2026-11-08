## ADDED Requirements

### Requirement: Keep-place setting

Settings SHALL expose, in the Bridge category, a toggle "Keep place on re-render" (`ui.rerender_keep_place` in the `[ui]` config section, default on, config.toml overridable) that gates the scroll and unsent-text kick deferral and the draft save-and-restore.

#### Scenario: Toggle off

- **WHEN** the user turns the toggle off
- **THEN** subsequent kicks, draft reports and restores behave as before the feature
