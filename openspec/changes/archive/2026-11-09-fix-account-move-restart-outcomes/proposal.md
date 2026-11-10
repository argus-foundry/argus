# Preserve account moves when restart outcomes are unknown

The approved rebase assignment carries the runtime safety rules into account moves. A kick RPC failure cannot establish which account the running process uses. Keep the persisted target, report an unknown restart, and probe accepted kicks before claiming success. Do not restore Hera closeout after an ambiguous start.

No new endpoint or frontend controls are introduced. The REST outcome applies to all clients. Existing TUI and MCP callers receive an error for an uncertain kick; the checked HTTP/MCP settlement belongs to the API service. Native macOS account move UI remains outside this fix.
