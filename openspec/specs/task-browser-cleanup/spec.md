# Task browser cleanup

## Purpose

Reclaim verified task-owned Playwright Chrome processes independently of the agent's process group, while preserving personal browsers and live tasks.

## Requirements

### Requirement: Browser ownership is verified before signaling

Cleanup SHALL require a recognized Chrome/Chromium executable, remote-debugging pipe, Playwright temporary profile or managed-worktree profile, and cwd within a managed task worktree. It SHALL re-read each captured browser/helper's argv and cwd before every signal, skip uncertainty, and never infer ownership from a shared Chrome support or Playwright cache directory alone.

#### Scenario: Personal Chrome remains running

- **WHEN** personal Chrome has no task-owned Playwright profile and managed task cwd
- **THEN** cleanup SHALL send it no signal, including when its arguments mention a worktree

#### Scenario: PID reused or ownership changed

- **WHEN** live argv or cwd differs from a captured identity
- **THEN** neither TERM nor KILL SHALL be sent to that PID

### Requirement: Full session and worktree cleanup includes browsers

Full session stops and worktree removal SHALL stop verified task browsers and captured helpers with SIGTERM then SIGKILL after five seconds. Agent-only/rerender stops SHALL preserve browsers. Escalation SHALL act only on the original snapshot and respect cancellation and an optional eligibility recheck.

#### Scenario: Browser escaped the session group

- **WHEN** a task session is fully stopped and its browser has a separate process group
- **THEN** the browser and captured helpers SHALL receive cleanup independently of the agent group

### Requirement: Finished and missing task browsers are swept safely

The daemon SHALL scan every ten seconds and stop browsers after six eligible ticks when every worktree claimant is finished or missing and none has a live session. Any task-list, running-list, process-scan, or ownership error SHALL prevent unsafe cleanup. Eligibility SHALL be rechecked before TERM and KILL.

#### Scenario: Live or resumed task browser survives

- **WHEN** a finished task has a live session or another unfinished task claims the worktree
- **THEN** the sweep SHALL preserve its browser

#### Scenario: Deleted task left a browser

- **WHEN** a verified task browser has no task row or live claimant for six consecutive eligible ticks
- **THEN** the daemon SHALL clean it up despite the missing row

### Requirement: Doctor reports task browser orphans without mutation

Doctor SHALL list task-owned browsers whose parent is PID 1, distinguish unavailable inspection from no findings, and send no signals.

#### Scenario: Process inspection unavailable

- **WHEN** the process scan fails
- **THEN** doctor SHALL report UNKNOWN without changing its exit-code policy
