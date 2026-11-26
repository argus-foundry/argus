## Why

A Git transport can keep its parent's output pipes open after the parent exits. Leased cleanup holds a repository lock, so cancellation must cover the entire command.

## What Changes

- Reuse bounded Git process-group execution for leased push and the origin lookup.
- Verify cancellation when a child process holds an inherited output pipe.
- Verify linked worktree registration and the current stack branch assignment under Git ownership.
- Apply successful Archive and Unarchive to a reopened detail when the task leaves the selected list.
