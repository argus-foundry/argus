## ADDED Requirements

### Requirement: Plugin task badge contract

The system SHALL treat a `task_meta` row with key `badge`, in any namespace, as a task badge whose value is a JSON object with fields `icon` (required), `tone` (optional: `neutral`, `ok`, `warn`, `alert`; default `neutral`) and `expires_at` (optional RFC3339). The system MUST ignore a badge whose value is not valid JSON, whose `icon` is not exactly one rune occupying one terminal cell, or whose `expires_at` is unparseable or has passed. A task SHALL show at most one badge: the valid badge from the alphabetically first namespace.

#### Scenario: Valid badge accepted

- **WHEN** a task has `badge` = `{"icon":"<one Nerd Font glyph>","tone":"ok"}` in namespace `iris`
- **THEN** the badge is parsed with that glyph and the `ok` tone

#### Scenario: Invalid or expired badge ignored

- **WHEN** the badge JSON is malformed, the icon is a two-cell colour emoji, or `expires_at` is in the past
- **THEN** no badge is shown for the task

#### Scenario: One badge per task

- **WHEN** namespaces `alpha` and `zeta` both carry a valid badge for one task
- **THEN** the `alpha` badge is used

### Requirement: Badge rendering in the TUI

The TUI SHALL draw the badge as a single glyph styled by its tone using existing theme styles, in the task list row immediately after the PR indicator cell and in the Hera rail row's right-aligned tag slot beside the PR tag. The rail row's name MUST truncate before the badge glyph is dropped. Expiry SHALL be evaluated at draw time. The badge is read from the existing tick via one `task_meta` query; no endpoint or polling is added.

#### Scenario: Task list shows badge

- **WHEN** a visible task has an unexpired badge
- **THEN** its row shows the glyph after the status and PR cells

#### Scenario: Rail keeps badge when narrow

- **WHEN** a rail row's name does not fit alongside its PR tag and badge
- **THEN** the name truncates and both the PR tag and badge glyph remain

### Requirement: TUI-only frontend scope

The web and macOS frontends SHALL ignore task badges, and the remote (`--remote`) TUI SHALL show none. This is an explicit exception to the three-frontend parity rule.

#### Scenario: Non-TUI surfaces unchanged

- **WHEN** a task carries a badge
- **THEN** the web SPA, macOS app and remote TUI render the task exactly as without it
