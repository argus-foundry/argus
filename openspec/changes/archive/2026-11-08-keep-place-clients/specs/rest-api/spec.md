## ADDED Requirements

### Requirement: Resize reports client-side keep-place state

`POST /api/tasks/{id}/resize` SHALL accept optional boolean `scrolled_up` and `composer_dirty` fields (absent means false) and SHALL return a `deferred` field: `"scrolled"`, `"draft"` or empty. When the resize would trigger the size-drift kill + resume, the daemon SHALL defer it when the composer holds unsent text (the Claude mod's report, else the rendered Claude composer read from the session's output tail when it is found, else `composer_dirty`) and otherwise when `scrolled_up` is true; the draft gate outranks the scroll gate. A deferral SHALL keep the task's cols cache invalidated so a later same-size resize re-evaluates, and SHALL be logged. `composer_dirty` alone SHALL stop holding after 10 minutes of deferrals at one width; the scroll gate is uncapped. The rendered-composer read SHALL run only while a kick is being evaluated. With `ui.rerender_keep_place` off both fields SHALL be ignored.

#### Scenario: Scrolled-up client

- **WHEN** a resize crosses the rerender margin with `scrolled_up` true and an empty composer
- **THEN** no kick is queued, the response carries `deferred: "scrolled"`, and a later same-size resize with `scrolled_up` false kicks

#### Scenario: Client flag only

- **WHEN** no mod report exists, the composer is not found on screen, and `composer_dirty` is true
- **THEN** the kick is deferred with `deferred: "draft"` until the flag clears or 10 minutes pass at that width

#### Scenario: Screen overrides a stale flag

- **WHEN** the rendered Claude composer is found empty and `composer_dirty` is true
- **THEN** the flag does not hold the kick

#### Scenario: Old client

- **WHEN** a resize omits both fields
- **THEN** behavior is unchanged from before this requirement

#### Scenario: Setting off

- **WHEN** `ui.rerender_keep_place` is off
- **THEN** both fields are ignored and no screen read occurs

#### Scenario: Steady state

- **WHEN** a resize does not cross the margin, or the cols are unchanged
- **THEN** the rendered-composer read does not run
