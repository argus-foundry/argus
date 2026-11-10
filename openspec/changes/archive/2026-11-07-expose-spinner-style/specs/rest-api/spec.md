## ADDED Requirements

### Requirement: Settings expose the configured spinner style

`GET /api/settings` SHALL include `display.spinner_style`, and `PUT /api/settings` SHALL accept an optional `display.spinner_style` of `progress`, `dots`, `braille` or `classic`, persisted to the same setting the TUI reads. A missing `display` SHALL leave it unchanged. An unknown value SHALL return 400 `{"error":"invalid spinner style","code":"invalid_spinner_style"}` with nothing written. A valid update SHALL return 200 with the full settings response.

#### Scenario: Round trip

- **WHEN** a client PUTs `display.spinner_style` `braille`
- **THEN** the response and the next GET report `braille` and the TUI config reads `braille`
