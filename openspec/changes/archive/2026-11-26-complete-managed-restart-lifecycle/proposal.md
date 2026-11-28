# Complete the managed restart lifecycle

## Why

A stream can remain bound across many managed restarts or miss a replacement that exits before the next attach. During connected Hera reclaim, the process may change before the stop reaches the owner.

## What Changes

Give each managed replacement a fresh attach retry budget and make its confirmed exit available to the daemon even when its stream was never attached. Keep process-scoped HTTP replay contiguous with live output. Require the owning process to accept the exact Hera reclaim target before removing its worktree.
