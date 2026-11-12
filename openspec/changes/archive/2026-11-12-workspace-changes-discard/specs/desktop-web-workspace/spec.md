## ADDED Requirements

### Requirement: Confirmed local file discard
The workspace SHALL offer local file discard only for dirty regular file entries, capture task and path before confirmation, initially focus Cancel and explain restoration to HEAD or explicit untracked deletion. It SHALL send scope local and explicit delete_untracked, never retry automatically, retain failures and list state, explain documented error codes, and refresh status and any open diff through worktree.changed.

#### Scenario: Adopt the merged contract
- **WHEN** Cancel sends no request; tracked restore sends false, untracked delete sends true only after confirmation, and conflict errors preserve the list.
- **THEN** the client follows the merged daemon contract, retaining captured-task cancellation and accessible feedback
