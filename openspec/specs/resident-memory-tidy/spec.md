# Resident memory tidy specification

## Purpose

Give the resident a quiet, config-controlled nightly pass to curate its own working memory.

## Requirements

### Requirement: Config-owned nightly timer

The daemon SHALL ensure exactly one durable built-in memory tidy timer when the resident is enabled. `resident.memory_tidy` SHALL default to `02:30` in local time, accept strict HH:MM, and accept `off` to disable. Reconciliation SHALL preserve unchanged overdue timers across restart, update changed schedules, and retract unsubmitted pending fires when disabled or changed. Invalid settings SHALL log an error and disable the built-in until corrected, retracting its unsubmitted fires while personal timers continue.

#### Scenario: Restart and config changes

- **WHEN** the daemon restarts with the same setting
- **THEN** the same timer and next due time remain, without duplicates or lost overdue fires
- **WHEN** the setting changes or becomes off
- **THEN** that built-in timer is updated or disabled and pending fires are retracted

#### Scenario: Invalid time leaves personal follow-ups working

- **WHEN** the tidy setting becomes invalid while its fire is queued
- **THEN** the built-in fire is retracted and the error is logged
- **AND** personal follow-up timers still fire normally

### Requirement: Protected ownership

Timer listing SHALL identify the built-in timer, Solo SHALL NOT cancel it through timer tools, and the built-in SHALL NOT consume Solo's personal timer allowance. Only config reconciliation SHALL change it.

#### Scenario: Solo cancels a built-in

- **WHEN** Solo cancels the built-in timer id
- **THEN** the store refuses with an explanation that config controls it

### Requirement: Quiet memory curation procedure

Every fire SHALL instruct Solo to curate only `resident.memory_dir` through KB tools: merge duplicates, prefer the newest evidenced fact in conflicts, remove stale or finished open loops and cancel their personal timers, keep files lean, fix broken links, preserve explicit instructions and preferences, retain required frontmatter, and keep a short dated change log. Solo SHALL remain quiet unless Aaron's attention is needed.

#### Scenario: Nightly fire

- **WHEN** the built-in timer is due
- **THEN** one durable inbox message carries the configured folder and full curation procedure
- **AND** missed fires coalesce through the existing resident timer delivery path

