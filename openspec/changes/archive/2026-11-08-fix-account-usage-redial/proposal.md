## Why

The TUI binds its per-account usage source to the daemon client dialed at startup. After any daemon bounce that client is dead ("connection is shut down"), so the `>` account picker and the `auto` target saw every account's usage as unknown and `auto` resolved to the over-budget default account.

## What Changes

- The TUI's account-usage read reconnects once over a fresh daemon connection when the bound client fails, as tier routing's usage read already does.

## Impact

- Capability `account-move`. TUI only; web and macOS read usage through `GET /api/accounts`, which is unaffected.
