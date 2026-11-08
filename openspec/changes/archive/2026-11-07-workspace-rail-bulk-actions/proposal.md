# Rail Select mode

## Why
Explicit task selection makes bulk actions reviewable without changing the viewer or widening the rail.

## What changes
Add tri-state select-all, project and coordinator cascades through collapsed rows, retained search selection with hidden counts, and Cancel to clear selection. All single and bulk task actions share one task-list confirmation and four-concurrent runner with Stop and progress. Large batches use cached risk groups in a fixed-height modal. Active offers neutral Archive; Archived offers red Delete.

## Non-Goals
No backend, REST, daemon, schema or static shell changes. No atomic batch, durable job, typed confirmation or Active bulk delete. `workspace-task-bulk-actions-parity` tracks TUI, original web and macOS parity. `workspace-rail-selection-accelerators` tracks modifier/range gestures.
