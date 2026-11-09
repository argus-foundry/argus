# Ensure Pi launches with Argus MCP

## Why
Pi's backend family currently supplies neither MCP launch flags nor global MCP injection, so Argus-launched Pi sessions cannot discover Argus tools automatically. Current Pi supports native MCP via `~/.pi/agent/mcp.json`.

## What Changes
- Register Argus's streamable HTTP MCP endpoint in Pi's native user-level MCP configuration using the backend family's global injection hook.
- Refresh registration before launch, preserving unrelated settings and servers; log success and failure without secrets.
- Test configuration creation, updates, preservation, malformed input, and backend launch wiring using temporary homes only.

## Non-Goals
- No REST API or frontend changes: TUI, web, and macOS task launches share backend wiring.
- No extension-based MCP bridge or changes to Pi's own implementation.
