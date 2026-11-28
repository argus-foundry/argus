# Serialize session mutations

## Why

A native resume and a daemon stop or resume can overlap across processes.

## What Changes

Serialize runner mutations through their persistence, have REST stop consult the session owner about pending starts, and suppress a deferred native restart after a confirmed stop.

The TUI, web, and macOS use the same daemon session owner. The REST stop response uses the existing restart conflict code. Native TUI deferred restarts use the persisted stop generation. The remote TUI sends width rerender and transcript switch requests through the same owner-managed routes as the web and macOS clients.
