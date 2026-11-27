## Why

PR #1245 added board `action` and `todo` kinds, but the Hera rail's coordinator envelope still paints orange for any unread attention and has no blue FYI-only state or red blocking state. Aaron approved coloring the envelope by the most urgent open item.

## What Changes

- The coordinator envelope keeps its glyph and takes its color from the most urgent open attention item: red for a blocking action, orange for a decision or ready non-blocking action, blue for FYI-only.
- Todos and trigger-pending actions are quiet: no envelope, no color.
- The rule reuses the shared attention predicate (`BoardAttention` / `AttentionUnread`); no parallel rule.
- The ctrl+g/cmd+g board jump is unchanged: it targets only coordinators with an unread decision, never one that has only FYIs or actions.

## Non-Goals

- Web SPA: its Projects/Hera rail renders no board envelope or decision/FYI indicator (the roster's `board_unread` is not drawn), so there is nothing to recolor and no SW_VERSION bump.
- macOS: the Hera tab is read-only and draws no envelope. Named follow-up: add the envelope there if/when the macOS Hera tab renders board attention.
- Changing the jump hotkey's targeting.

## Impact

- `internal/hera/model` (level + jump counts), `internal/tui/hera` (rail marker color, jump gate), theme reuse only (`ColorError`, `ColorInProgress`, `ColorInReview`).
