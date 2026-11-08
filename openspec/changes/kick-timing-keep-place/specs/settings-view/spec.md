## ADDED Requirements

### Requirement: Keep-place setting

Settings SHALL expose a Bridge toggle "Keep place on re-render" (`bridge.keep_place`, default on, config.toml overridable) that gates the scroll and unsent-text kick deferral and the draft save-and-restore.

#### Scenario: Toggle off

- **WHEN** the user turns the toggle off
- **THEN** subsequent kicks, draft reports and restores behave as before the feature
