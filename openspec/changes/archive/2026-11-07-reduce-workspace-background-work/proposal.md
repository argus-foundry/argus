# Reduce optional workspace background work

## Why
Unchanged events and roster snapshots replace the complete rail. Hidden tabs continue context and fallback snapshot reads.

## What Changes
Skip unchanged rail inputs and coalesce asynchronous rail updates per animation frame. Pause timer polls in hidden documents and refresh once on visibility return, keeping SSE and immediate archive input fencing.

## Impact
Frontend only in the optional web workspace. No REST contract changes or TUI/macOS changes required.

## Abstraction inventory
TaskNavigation owns rendered rail inputs and pending frame work. ComposerContext owns selected-task usage reads. WorkspaceEvents owns the fallback snapshot timer independently of SSE. WorkspaceConnection owns its legacy no-SSE timer.
