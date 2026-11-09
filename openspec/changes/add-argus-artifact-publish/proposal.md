## Why

`add-artifact-home-account` tells an agent which account its project's claude.ai artifacts belong on, and to publish through an external `artpush` CLI when it is running on a different account. That CLI was never built. A spike showed that the Argus daemon can do the publishing itself:

- A `claude setup-token` token can publish to the claude.ai artifacts endpoint without any model run.
- An account at 0% budget is refused. Its tool list is cut down and every call gets "not available to this session".

So the publisher belongs in Argus, and it needs an explicit path for when the home account is unavailable.

## What Changes

- New `[artifact_tokens]` config: account name → secret source (for example `op://claude/shell-env/CLAUDE_SETUP_TOKEN_PERSONAL`). The daemon resolves it at publish time and never exposes it to agents.
- New daemon-side publisher (`internal/claudeartifacts`): a stateless JSON-RPC client for `https://api.anthropic.com/api/artifacts/mcp`. It publishes a single HTML page with `publish_version`, updates an existing artifact by looking up its current version, and recognizes the "not available" refusal as *account unavailable*.
- New MCP tool `artifact_publish` (path, title, account, optional update link) that returns the claude.ai link. When the account is unavailable, it saves the page as a task artifact and reports which other configured accounts can still publish.
- New MCP tool `artifact_open` that opens a task's registered artifact in the default app, or reveals it in Finder.
- New REST endpoint `POST /api/artifacts/publish` and CLI subcommand `argus artifact publish <file> --account <name> [--title T] [--update <url>]` for a person to republish by hand.
- **MODIFIED** `artifact-home-account` routing guidance:
  - It names the `artifact_publish` tool instead of `artpush`.
  - When the home account is unavailable, the agent asks the user, using its question tool, whether to publish to an available account, open the page in the browser, reveal it in Finder, or park it for later.
- Every `artpush` mention is removed from docs and code comments.

## Capabilities

### New Capabilities

- `artifact-publishing`: per-account artifact tokens, the daemon-side publisher, the `artifact_publish` and `artifact_open` MCP tools, the REST endpoint and CLI subcommand, and unavailable-account handling.

### Modified Capabilities

- `artifact-home-account`: the routing-guidance requirement now names the `artifact_publish` tool and the four-option user question for an unavailable home account.

## Impact

- New package `internal/claudeartifacts`.
- `internal/config`: `ArtifactTokens` map and config.toml merge.
- `internal/mcp`: `artifact_publish` and `artifact_open`, next to `artifact_register` in `artifacts.go`.
- `internal/api`: the publish endpoint.
- `cmd/argus`: the `artifact publish` subcommand.
- `internal/routing/builtin/argus-artifacts.md`.
- Docs: README Reference (MCP tools, REST endpoints, config), `gotchas/misc.md`, `backends.md`.
- Comments in `internal/agent` and `internal/daemon/surface.go` that mention artpush. The routing snippet is not in the supervisor spawn surface, so no digest change is expected; confirm during implementation.
- Frontend parity: the publish endpoint is reachable over REST. A Publish button in the TUI, web and macOS clients is a **named follow-up**. No client gets one in this change.
- External: Anthropic's undocumented artifacts endpoint. It may change without notice, so failures must be loud.
