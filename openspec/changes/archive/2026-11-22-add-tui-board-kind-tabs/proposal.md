## Why

The coordinator board mixes decisions, FYIs, actions and todos. Matching the Decide page's tabs makes each kind easy to find in the TUI.

## What changes

- Add ordered kind tabs with open counts, colored badges and clamped arrow navigation.
- Filter every board action through the selected tab, remembering it per coordinator for this session.
- Document and test the rebindable keys and narrow rendering.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `hera-view`: coordinator board kind tabs.

## Impact

TUI board, theme, keymap and help only. No REST changes. Web already provides tabs.
