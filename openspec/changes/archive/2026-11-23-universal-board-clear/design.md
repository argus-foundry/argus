## Context

The board currently refuses x on actions and drops todos, while d completes both. The existing completion callback already carries the selected item revision to the durable batch operation.

## Goals / Non-goals

- Goal: x clears every kind using its existing durable operation, with actions and todos completed as done.
- Non-goal: Change REST, web Decide controls or macOS read-only Hera. Todo dropping remains available on web Decide; the standing macOS Hera mutation follow-up remains unchanged.

## Decisions

- Route actions and todos from the dismiss action to existing done completion; retain the approval-first branch and generic decision/FYI dismissal.
- Remove hera_board.done from the registry entirely. Existing unknown-action handling silently ignores old overrides, including invalid keyspec values.
- Generate clear help from the active keymap and retain concise hints that fit more board widths.
- Log selected clear outcomes without commands; asynchronous handlers continue logging success/failure and revision conflicts.

## Risks / Trade-offs

- Accidental x completes a todo rather than dropping it → document this intentional meaning and retain browser dropping.
- Old bindings become inert → test real config loading plus keymap resolution and help.
