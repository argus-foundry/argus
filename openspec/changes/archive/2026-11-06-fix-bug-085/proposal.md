## Why

BUG-085: the persistent Tasks rail stacks a redundant task-name chip beneath the navigation tabs. The selected task is already highlighted in the rail, so this consumes a content row without helping navigation.

## What Changes

- Omit the agent task-name header when the Tasks rail is visible and give its row to the content.
- Preserve the classic agent header when the rail is disabled.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `persistent-task-rail`: require a single navigation header row above the rail content.

## Impact

TUI layout composition and SimulationScreen regression coverage only. No REST contract changes; web and macOS do not use this TUI header and require no changes.
