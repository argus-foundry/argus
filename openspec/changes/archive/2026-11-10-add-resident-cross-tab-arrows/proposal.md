## Why

The resident sits left of Tasks in the header but cannot be reached through the continuous modified-arrow chain. Include it so spatial navigation matches the visible layout.

## What changes

- Extend the enabled chain through the resident rail, conversation and worker panes.
- Enter resident from Tasks at its rightmost present pane and return to the Tasks list or persistent rail.
- Keep disabled, remote, modal, filtering, fullscreen and persistence behavior unchanged.
- Describe the resident in Appearance details and README Reference.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `tui-shell`: extend the cross-tab chain to the resident.
- `resident-tui`: describe directional entry and internal traversal.
- `persistent-task-rail`: describe its left neighbor.
- `settings-view`: describe the complete chain.

## Impact

TUI shell navigation and documentation only; no REST changes. Web and macOS have no corresponding terminal pane chain and require no changes.
