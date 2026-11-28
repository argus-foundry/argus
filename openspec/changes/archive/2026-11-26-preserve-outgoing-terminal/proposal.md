# Preserve outgoing terminal ownership during handoff

## Why

The supervisor can list a restart before the outgoing exit callback publishes its receipt. Theia can also reopen cached recorded output after new bytes arrive while it is suspended.

## What Changes

- Capture the outgoing PID when the owner accepts a restart.
- Refresh recorded terminal history when the snapshot has no process identity.
- Fence reunions and explicit launches while an archived Hera task still owes its scoped stop.
- Use owner-aware reclaim from the REST nuke path and wait for retirement in standalone mode.
- Reject processless SSE attachments and keep remote terminals reconnecting when the owner reports a pending replacement.
