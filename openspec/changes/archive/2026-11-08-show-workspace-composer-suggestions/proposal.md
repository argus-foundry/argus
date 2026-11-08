## Why

The empty Theia composer should show the provider's suggested next prompt without changing a person's draft or sending input.

## What Changes

Read the selected task's bridge suggestion and show its text only as the empty textarea placeholder. Fence and cancel reads on selection, disconnect and newer refreshes. Refresh on suggestion and task/session events and periodic authority checks. Clear on provider clear, new turn, session replacement, unavailable reads or expired bridge authority. Keep draft restoration and explicit send behavior unchanged.

## Non-Goals

No accept shortcut or fill button. No terminal scraping, daemon, store or API changes. TUI suggestion presentation is deferred to `tui-workspace-interaction-parity`, and macOS suggestion presentation to `native-workspace-rebuild`; the existing REST contract is unchanged. The existing static PWA shell is unchanged.
