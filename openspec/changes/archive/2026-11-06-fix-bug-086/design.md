## Context

The optional rail admits project and Archive headers as selection targets, unlike the classic task list. Both cursor navigation and rebuild clamping inherit this rule.

## Goals / Non-goals

- Keep rail selection on a task through navigation, refresh, filtering, mode changes, deletion and archiving.
- Preserve classic list behavior and avoid the concurrently edited app header.
- No REST, web or macOS behavior changes.

## Decisions

- Reuse the classic list's header-skipping traversal, including expanding a project before selecting its first or last task. Merely changing the selectable predicate would make folded projects unreachable.
- Drop manual rail header folding and its Space trigger. Enter always targets tasks. Focus-fold and `F` govern project expansion.
- Navigation into Archive opens it; the rail retains the open Archive when leaving it. If only archived tasks remain, open Archive so selection is possible.
- Repair expansion targets when their project disappears, preventing rebuilds containing only collapsed headers despite admitted tasks.

## Risks / Trade-offs

- Shared traversal could change classic behavior → run existing classic tests alongside rail regressions.
- Removing Space changes a documented structural key → update static help, its assertion, and README; no rebindable keymap action exists to remove.
