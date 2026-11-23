## Why

Repeated presses of x leave actions on the coordinator board because completion has a separate d key. Aaron requested one universal clear key so handled work goes away predictably.

## What changes

- Make x complete actions and todos as done, dismiss FYIs and decisions, and preserve resident approval denial.
- Remove the separate done action and default d binding; ignore old done overrides through existing unknown-action handling.
- Update generated help, hints and Reference documentation.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `coordinator-board`: Universal clear controls and hints.
- `hera-view`: Board interaction consistently delegates clear by item kind.
- `keybindings`: Removed done overrides remain harmless and help shows only clear.

## Impact

TUI board dispatch, keymap, tests and docs. No REST contract change. Web Decide keeps todo dropping; macOS retains its standing read-only Hera gap. This mission explicitly authorizes the requested behavior and implementation.
