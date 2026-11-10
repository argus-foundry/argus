## ADDED Requirements

### Requirement: Spinner style is readable and writable over REST

The spinner style selected in the TUI Settings view SHALL be the same persisted value exposed as `display.spinner_style` over REST.

#### Scenario: Shared value

- **WHEN** the style is changed over REST
- **THEN** the TUI reads the new style from its existing setting
