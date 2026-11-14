## ADDED Requirements

### Requirement: Detached task status glyph is greyed

A task row's status glyph SHALL render in the dimmed grey style when the task is detached (in_progress or in_review, not archived, session absent from the daemon alive set for 2 consecutive ticks; fail-open on an unreadable session list). The glyph itself SHALL be the one the task would otherwise show; only its colour changes. An animated spinner for a detached task SHALL render as a static glyph.

#### Scenario: Detached in-review task

- **WHEN** an in-review task has no live session past the grace ticks
- **THEN** its row shows the in-review glyph in grey

#### Scenario: Detached task does not spin

- **WHEN** a detached in-progress task would otherwise render the actively-running spinner
- **THEN** a static grey glyph is shown with no animation
