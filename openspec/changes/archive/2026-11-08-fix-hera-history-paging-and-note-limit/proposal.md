## Why

History pages scan unrelated mail while holding the store lock, and valid escaped dismissal notes can exceed the encoded request limit.

## What Changes

Add the full recipient/message index during schema initialization and use separate cursor and first-page queries. Allow the worst JSON expansion of a 64 KiB decoded note while preserving a bounded request body and decoded byte validation.

## Non-Goals

TUI-originated events do not reach daemon SSE; tracked follow-up: ENG-1125, forward TUI events to the daemon. This PR documents the gap without implementing a relay. Existing web roster polling refreshes board_unread. macOS remains excluded.
