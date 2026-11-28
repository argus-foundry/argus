## Why

The board tab count fill touches its label, and bold text alone makes selection hard to see. Aaron requested a filled selected tab with a plain gap before unselected counts.

## What changes

- Render the selected tab as one padded block in its kind color with contrasting ink.
- Keep one plain space between unselected labels and count badges, and preserve bounded clipping around selection.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `hera-view`: Coordinator board kind tabs rendering.

## Impact

TUI board tab rendering and SimulationScreen tests only. No REST, web or macOS contract changes.
