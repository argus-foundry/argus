# Settle startup attachment and reclaim transitions

## Why

A daemon can join a session while the supervisor is publishing an exit. Reclaim also crosses task and binding state changes, and the Theia terminal consumes the same PID-scoped output API as the legacy web terminal.

## What Changes

- Associate pending launch snapshots with the managed predecessor and settle retained exit receipts from startup.
- Coordinate reclaim with restore, pin and standalone Hera cleanup.
- Keep Theia terminal reconnect and cached scrollback tied to the producing process.
