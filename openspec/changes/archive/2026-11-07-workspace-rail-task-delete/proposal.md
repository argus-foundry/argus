# Rail task deletion

## Why
Task deletion needs a direct, deliberate route from both task rail views.

## What changes
Add a separated red Delete menu action and extend the shared confirmation with a danger treatment and captured task details. Use existing REST reads for advisory live, Hera, changes and PR observations. Confirmed deletion removes the row locally, detaches its viewer and selects a neighbor while preserving other drafts.

## Non-Goals
No backend or wire changes. No cleanup completion guarantee. `workspace-task-bulk-actions-parity` tracks TUI, original web and macOS bulk parity; their established single deletion paths remain.
