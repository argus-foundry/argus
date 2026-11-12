## ADDED Requirements

### Requirement: Worktree change events

The daemon SHALL emit `worktree.changed` with the task id and payload `{"revision":"<opaque>","reason":"filesystem|git|discard","paths":[...]}` (revision and paths optional). A consumer receiving an event without a revision SHALL treat it as an invalidation. Payloads carry no file contents.

#### Scenario: Replay
- **WHEN** a client reconnects with `since` before a worktree event
- **THEN** the ring replays it, or a `resync` tells the client to refetch status
