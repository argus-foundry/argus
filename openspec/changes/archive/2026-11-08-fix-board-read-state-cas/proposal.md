## Why

An independent writer can change a board item after read-state validation but before its conditional UPDATE. Ignoring the affected-row count reports a successful acknowledgment and emits an event despite losing the revision comparison.

## What Changes

Require the conditional UPDATE to affect a row. Otherwise re-read the role-scoped item and return revision conflict, resolved or not found without a success event. Cover cross-connection races and audit the remaining board writes.

## Non-Goals

No macOS changes or TUI event relay. The existing ENG-1125 follow-up remains deferred.
