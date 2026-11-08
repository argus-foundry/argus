## ADDED Requirements

### Requirement: The mod reports the composer draft

When `ui.rerender_keep_place` is on, the mod SHALL report the Claude prompt-box draft to the daemon as a `draft` event carrying text and cursor, debounced to 300 ms, with an emptied box reported immediately. The daemon SHALL expose only a boolean `has_draft` in REST/state JSON and SHALL NOT serialize the draft text.

#### Scenario: Typing is reported

- **WHEN** the user types into a bridged session's composer
- **THEN** within the debounce the daemon holds the draft and `has_draft` is true

#### Scenario: Submit clears the draft immediately

- **WHEN** a submit empties the composer
- **THEN** an empty draft is reported without waiting for the debounce and `has_draft` becomes false

#### Scenario: Draft text is never serialized

- **WHEN** a client reads `GET /api/tasks/{id}/bridge`
- **THEN** the response contains `has_draft` and no draft text

### Requirement: A resumed session gets its pending draft back once

On `session.start` the mod SHALL ask the daemon for a pending draft. The daemon SHALL return it only when the asking boot differs from the draft's boot, the draft is non-empty, younger than two minutes, and untaken; returning it marks it taken. The mod SHALL fill it with `$.prompt.fill` (mode replace, original cursor) only when `$.prompt.read()` shows an empty box, and SHALL report the outcome.

#### Scenario: Kick restores the draft

- **WHEN** a session with draft "fix the bug" is killed and resumed within two minutes
- **THEN** the new mod fills "fix the bug" into the empty box and the daemon marks the draft taken

#### Scenario: User already typed

- **WHEN** the new box is non-empty when the draft arrives
- **THEN** the mod does not overwrite it and reports `skipped_nonempty`

#### Scenario: Stale or taken draft

- **WHEN** the draft is older than two minutes or was already returned
- **THEN** the daemon returns no draft

#### Scenario: Fail open

- **WHEN** the daemon is old, unreachable, or the fill is refused
- **THEN** the session starts normally with an empty box

#### Scenario: Setting off

- **WHEN** `ui.rerender_keep_place` is off
- **THEN** the daemon ignores `draft` events and returns no draft
