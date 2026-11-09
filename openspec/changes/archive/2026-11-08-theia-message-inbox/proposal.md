# Add read-only Agent messages and a separate coordinator human Inbox.

## Scope

ENG-1100, ENG-1118. Frontend only, local commits.

## Non-Goals

No daemon, SPA or macOS changes. Named follow-ups are SPA adoption of theia-message-inbox and macOS adoption of theia-message-inbox; existing TUI behavior remains unchanged. No live daemon verification.

## Abstraction inventory

AgentMessages owns read-only history. CoordinatorInbox owns board revisions and explicit human actions. TaskTools selects independent views. Composer owns drafts and human send. Rail mail opens the matching coordinator Inbox.

Slice 1 supplies the shared event foundation and reverts last; slices 2 and 3 can be reverted independently.
