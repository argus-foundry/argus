## Why

Review found duplicate events when reopening read board items and raw store diagnostics in history error responses.

## What Changes

Suppress board events for repeated legacy reads. Return generic history failures and log store diagnostics. Add regression coverage for committed events, history identity and cursors, pagination defaults and durable notices.

## Scope

This implements the explicitly assigned review fixes. No client controls or macOS changes. The existing frontend follow-ups remain unchanged.
