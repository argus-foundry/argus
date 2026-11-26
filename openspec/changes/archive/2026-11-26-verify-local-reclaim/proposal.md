## Why

An attempted local Git deletion may leave the branch intact. Reclaim must verify the result before settling the local step or deleting a remote ref.

## Impact

This applies to the shared daemon cleanup used by resident deletion and its recovery sweep.
