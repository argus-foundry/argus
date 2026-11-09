## Why

The `>` picker hid the task's current account, so with two accounts it offered only `auto` plus one target. Aaron asked for `auto` plus all accounts.

## What Changes

- Picker lists every account, current marked, with usage and a near-limit warning (>=80%, still selectable).
- Moving to the current account restarts in place (no copy, pins) instead of erroring.

## Impact

Capability `account-move`. TUI, REST and MCP share the backend change. Web and macOS have no move UI today (named gap, unchanged).
