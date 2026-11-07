# Workspace creation model choices

Limit model choices to backend metadata so a requested model is supported. Preserve explicit choices even when equal to the backend default. Require an explicit backend for model overrides because inherited routing cannot be resolved from project and global metadata.

## Abstractions

Creation backend identifies explicit metadata. Creation model policy validates and normalizes the request. NewTaskView renders the policy and retains project drafts.

## Scope

Optional web workspace only. REST contracts, TUI and macOS behavior are unchanged; no parity follow-up is required for this workspace form correction.
