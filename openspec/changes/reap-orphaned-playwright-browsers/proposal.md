## Why

Every time an agent opens a browser through Playwright MCP (`@playwright/mcp --isolated`), a separate Chrome instance appears as its own dock icon. When the agent's session ends, that Chrome often survives, and the user has to force-quit each one by hand. Playwright launches the browser with `detached: true`, so it leads its own process group and Argus's process-group stop never reaches it. The only thing that closes it is Playwright's own exit cleanup, and that never runs when the Playwright server is SIGKILLed. This can happen through Argus's 5-second escalation, Claude Code's MCP teardown, or a crash. After that, nothing that remains can see the browser or drive it.

## What Changes

- The daemon gains a lightweight sweeper that runs every 5 seconds. It finds automation-driven Chrome browsers launched under an Argus agent whose launcher has died (reparented to launchd), and closes them: SIGTERM to the browser's process group, then SIGKILL if it is still alive after a grace period.
- Ownership is established two ways. The first is recording browsers while their ancestry still visibly reaches an Argus process; this covers Codex agents, which strip `ARGUS_TASK_ID` from MCP server environments. The second is an `ARGUS_TASK_ID` in the browser's own environment, which covers browsers already orphaned before the sweeper saw them, such as across a daemon restart.
- Each tick costs one in-process `kern.proc.all` sysctl. Command lines are read only for unclassified Chrome main processes, and classifications are cached by PID and start time. There is no subprocess spawn and no per-process walk of the whole table.
- A new `[browsers] close_orphaned` config.toml key (default `true`) turns the sweeper off and restores today's behavior.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `agent-execution`: adds a requirement that orphaned automation browsers launched under Argus agent sessions are closed, scoped to Argus-owned browsers, cheap enough to run every few seconds, and configurable.

## Impact

- **Code:**
  - New `internal/agent/orphanbrowser*.go`: a darwin process scan via `golang.org/x/sys/unix`, ownership and orphan classification, and identity-checked signalling. Other platforms get a no-op build.
  - New `internal/daemon/browserreap.go`: tick loop and wiring.
  - `internal/config`: new `BrowsersConfig`.
- **Dependencies:** `golang.org/x/sys` is already in `go.mod`. No new dependencies.
- **Surfaces:** no REST, MCP, TUI, web or macOS changes. The daemon acts silently and logs each close.
- **Docs:**
  - A new gotcha bullet in `context/knowledge/gotchas/daemon-rpc.md` covering Playwright's detached browser group, `ppid == 1` alone not identifying an orphan (every app launched from the Dock has it), and Codex env stripping.
  - The README Reference config table gains the new key.
