## Why

Voice clients need an actionable result when a saved task ID no longer exists. Voice sends must follow the existing terminal rule against forwarding Ctrl+Z.

## What Changes

- Report a missing explicit task ID as a missing workflow.
- Reject Ctrl+Z in the voice send path before accepting a job.
