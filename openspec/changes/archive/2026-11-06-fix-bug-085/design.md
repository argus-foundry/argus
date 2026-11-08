## Context

The Tasks rail embeds the classic agent page, which includes its own task-name header. The rail also keeps the root navigation tabs visible, producing two stacked header rows.

## Goals / Non-Goals

**Goals:**

- Show only the navigation tabs above rail content and reclaim the task-name row.
- Preserve classic layout and live preference transitions.

**Non-Goals:**

- Change web or macOS layouts, REST behavior, keybindings, or session lifecycle.

## Decisions

Embed the shared agent panels directly in the rail, while the classic page retains its header wrapper. This avoids mutating shared header visibility on every entry, pending launch, or preference toggle. Resizing the header to zero would require guarding its renderer too, since it currently paints even with zero height.

## Risks / Trade-offs

- Geometry changes by one row → assert rendered panel position and PTY dimensions through the real event loop.
- The task-header clipboard hint shares the omitted row → the rail omits the whole redundant header as requested; clipboard staging and shortcut behavior remain unchanged.
