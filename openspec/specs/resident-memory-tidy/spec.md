# Resident memory tidy specification

## Purpose

Give the resident a quiet, config-controlled nightly pass to curate its own working memory.

## Requirements

### Requirement: Config-owned nightly timer

The daemon SHALL ensure exactly one durable built-in memory tidy timer when the resident is enabled. `resident.memory_tidy` SHALL default to `02:30` in host local time, accept strict HH:MM, and accept `off` to disable. The host zone SHALL be resolved from a valid TZ first, then the /etc/localtime symlink suffix after zoneinfo/. The stored timezone MUST be a concrete loadable name, never Local. If neither source resolves, the daemon SHALL log the error and disable only the built-in, without silently falling back to UTC. Reconciliation SHALL preserve unchanged overdue timers across restart. Changes to the expression, resolved timezone or enabled state SHALL recompute due time and retract unsubmitted pending fires. Disable SHALL store due_at=0. A note-only change SHALL preserve due time and pending deliveries, updating instructions for future fires only. Invalid settings SHALL log an error and disable the built-in until corrected, retracting its unsubmitted fires while personal timers continue. Any built-in reconciliation error SHALL be logged without aborting personal timer firing or delivery.

Built-in identity SHALL be retained while disabled, including during pruning of terminal personal timers.

#### Scenario: Restart and config changes

- **WHEN** the daemon restarts with the same setting and resolved timezone
- **THEN** the same timer and next due time remain, without duplicates or lost overdue fires
- **WHEN** the schedule or resolved host timezone changes, or the setting becomes off
- **THEN** that built-in timer is updated or disabled and pending fires are retracted

#### Scenario: Invalid time leaves personal follow-ups working

- **WHEN** the tidy setting becomes invalid while its fire is queued
- **THEN** the built-in fire is retracted and the error is logged
- **AND** personal follow-up timers still fire normally

#### Scenario: Unresolvable zone or reconciliation error

- **WHEN** neither host timezone source resolves, or built-in reconciliation fails
- **THEN** the daemon logs the error and personal timer firing and delivery continue
- **AND** an unresolved zone disables the built-in without a UTC fallback

#### Scenario: Instruction-only refresh

- **WHEN** only instruction text or memory_dir changes
- **THEN** due_at and the existing pending message remain unchanged
- **AND** subsequent fires use the refreshed note

#### Scenario: Disabled built-in survives pruning

- **WHEN** terminal personal timers older than the retention period are pruned
- **THEN** the disabled built-in retains its identity and can be re-enabled with the same id

### Requirement: Protected ownership

Timer listing SHALL identify the built-in timer, Solo SHALL NOT cancel it through timer tools, and the built-in SHALL NOT consume Solo's personal timer allowance. Only config reconciliation SHALL change it.

#### Scenario: Solo cancels a built-in

- **WHEN** Solo cancels the built-in timer id
- **THEN** the store refuses with an explanation that config controls it

### Requirement: Quiet memory curation procedure

Every fire SHALL instruct Solo to curate only `resident.memory_dir` through KB tools: merge duplicates, prefer the newest evidenced fact in conflicts, remove stale or finished open loops and cancel their personal timers, keep files lean, fix broken links and update references only within the memory folder, preserve explicit instructions and preferences, retain required frontmatter, and keep a short dated change log. When explicit preferences conflict, the older preference SHALL remain with a dated "superseded by …" annotation rather than being deleted. Solo SHALL remain quiet unless Aaron's attention is needed.

#### Scenario: Nightly fire

- **WHEN** the built-in timer is due
- **THEN** one durable inbox message carries the configured folder and full curation procedure
- **AND** missed fires coalesce through the existing resident timer delivery path

#### Scenario: Conflicting explicit preferences

- **WHEN** explicit preferences conflict during curation
- **THEN** the older preference is retained with a dated superseded-by note and references are updated only within the memory folder
