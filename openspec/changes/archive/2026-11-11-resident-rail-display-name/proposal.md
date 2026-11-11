## Why

The resident rail calls the agent `resident` while the tab and conversation call it by its configured name, such as Solo. The rail should follow that display name, including live Settings renames.

## What changes

- Show the validated `resident.name` on the resident tab's coordinator row, falling back to `resident` for invalid or empty names.
- Refresh that label through the existing config refresh paths without restarting or reattaching a session.
- Preserve worker labels and all stored Hera identities.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `resident-tui`: The resident tab's own rail row uses the configured display name and follows renames.

## Impact

TUI display code and render tests only. No REST, DB, MCP or worker-prompt changes. Projects and the jump switcher use the generic roster, which excludes the resident; their labels stay unchanged. Web and macOS have no changed REST contract or resident-tab rail surface to mirror.
