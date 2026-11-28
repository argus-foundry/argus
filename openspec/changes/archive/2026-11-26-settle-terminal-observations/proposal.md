# Settle terminal observations and durable reclaim

## Why

The daemon can observe a normal launch between PTY retirement and publication of its exit receipt. Browsers can also see a stale snapshot during managed replacement. Reclaim must protect the retired process and the task row throughout cleanup.

## What Changes

- Take the owner slot and last exit snapshot together and wait for a scoped exit.
- Keep PWA recovery in one bounded retry loop.
- Fence relaunch and generic cleanup while Hera owns the task.
- Retry deferred archived nuke stops and settle an expired receipt using the owner's exact retired PID.
