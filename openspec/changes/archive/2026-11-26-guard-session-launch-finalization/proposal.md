# Preserve session state across a concurrent stop

## Why

A stop can overlap launch persistence across REST and the native TUI. SQLite writer contention can also prevent an already launched session from being recorded.

## What Changes

- Refuse a REST stop of a Pending task without a session with the existing agent-restarting conflict code.
- Fence launch persistence against an intervening stop and acquire SQLite write intent before reading launch state.
- Bind TUI create and fork completion to the session returned by the creation flow.

The web and macOS apps receive the existing REST error shape. TUI create and fork completion changes apply to the native terminal client.
