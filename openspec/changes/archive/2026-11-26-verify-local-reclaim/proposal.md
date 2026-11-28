## Why

An attempted local Git deletion may leave the branch intact. Reclaim must verify the result before settling the local step or deleting a remote ref.

## What Changes

- Verify local removal and recheck ownership before leased branch deletion.
- Filter stacks by Git repository and reconcile a confirmed PWA delete.

## Impact

This applies to the shared daemon cleanup used by resident deletion and its recovery sweep.
