## Why

BUG-086 lets the Tasks rail cursor stop on project headers and leaves the content pane empty despite visible tasks. Task navigation must always select a task.

## What changes

- Skip every header during rail navigation and cursor restoration.
- Remove manual rail header folding; use focus-fold and `F`, automatically opening Archive when navigation enters it.
- Preserve the classic list's existing task-only navigation.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `persistent-task-rail`: Require task-only selection and a populated preview while tasks are visible.
- `tasks-project-expansion`: Clarify rail focus-fold and Archive navigation without selectable headers.

## Impact

TUI taskview row selection, help, README Reference, unit and SimulationScreen tests. No REST contract changes; web and macOS do not use the TUI cursor model and require no changes.
