## Why

Agents often say things like "when we release, remember to do X". Today that note stays in the chat scrollback, and it's lost once the session recycles or the task ends. The coordinator board is already the durable home for Aaron's attention items, but there's no quick way for Aaron to tell an agent to put a specific item there.

## What Changes

- New builtin skill `/hera-remind-me` (`internal/skills/builtin/hera-remind-me/SKILL.md`), injected into every Argus session like the other builtins.
- Invoked with optional free text (`/hera-remind-me the release step`). With no text, the agent uses the most recent actionable follow-up from the conversation.
- The agent turns the item into a board entry: a one-line subject, then a body with the action, what triggers it ("when we release"), why it matters, and any references (PR, branch, file, task). The kind is `fyi` unless Aaron has to choose something, in which case it's `decision`.
- **Coordinator session:** the agent checks open items with `hera_board_list`. If an open item already covers this, it updates that item with `hera_board_update`. Otherwise it creates one with `hera_board_post`.
- **Worker / freelance session:** board tools are coordinator-only, so the agent relays the item to its coordinator with `hera_send` and asks the coordinator to post it. The coordinator stays the board's only writer.
- **Plain task (no Hera binding):** the agent reports that there's no board in this session and stops. There is no fallback destination.
- No daemon, MCP, REST, or UI changes. Board ownership rules are unchanged.

## Capabilities

### New Capabilities

### Modified Capabilities

- `coordinator-board`: adds a human-directed capture requirement — Aaron can tell any Hera-bound agent to put a conversation item on the coordinator board, with relay from non-coordinator roles.

## Impact

- New file `internal/skills/builtin/hera-remind-me/SKILL.md` (embedded via the existing `go:embed builtin` tree; no wiring changes).
- `internal/skills/builtin_test.go` / `builtin_items_test.go` expected-name lists gain `hera-remind-me`.
- README Reference appendix: mention the new builtin skill if a builtin-skill table exists.
- Frontend parity: not applicable — no REST-exposed surface changes; the board items it creates already render on every frontend that renders the board.
