## Why

The resident can post reminders to its Hera board, but the resident tab neither loads those items nor lets Aaron open the board. Unread reminders also fail to raise the resident's attention marker.

## What changes

- Load the standing coordinator's board and enable the existing board controls on the resident tab.
- Auto-show unread arrivals, count unread items in the Details title, and compose replies into the resident conversation.
- Include unread board items in the resident header and rail-box attention signal.
- Document the board controls in resident help and the command palette while preserving the existing management restrictions.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-tui`: expose the resident board and include its unread items in attention signals.

## Impact

The scoped Hera reader, shared board view, resident attention poll, keymap help, palette, and TUI tests change. No REST contract changes; the existing `add-resident-web` follow-up continues to cover resident web/macOS parity. Aaron reported this bug directly and authorized implementation without a separate approval round.
