## Why

A live linked worktree still identifies its actual Git repository after project settings change. A cleanup lease also needs to distinguish a replacement repository installed at the original path.

## What Changes

- Prefer the actual linked worktree when checking surviving task ownership.
- Retain the original Git common-directory identity and origin destination through deferred cleanup.
- Carry the original identity through resident stack discovery and admission.
