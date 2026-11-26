# Settle scoped lifecycle operations

## Why

Review found races in terminal recovery and managed lifecycle operations that cross the daemon and supervisor boundary.

## What Changes

- Keep reconnect cursors associated with the original process through attachment.
- Check managed restart support before writing a backend handoff.
- Retry interrupted Hera stops for eligible archived tasks and their exact process. Coordinate new bindings with reclaim.
- Keep startup tracking active while the owner confirms a launch reservation.
