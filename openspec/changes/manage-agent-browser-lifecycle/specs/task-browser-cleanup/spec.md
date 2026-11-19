## MODIFIED Requirements

### Requirement: Browser ownership is verified before signaling

Cleanup SHALL require a recognized Chrome/Chromium executable, remote-debugging pipe, Playwright temporary profile or managed-worktree profile, and cwd within a managed task worktree. Recognition SHALL cover exact selected bundled headless/full executable and helper names, including Chrome for Testing, without accepting arbitrary command substrings. It SHALL re-read each captured browser/helper's argv and cwd before every signal, verify OS process start identity and managed generation where available, skip uncertainty, and never infer ownership from a shared Chrome support or Playwright cache directory alone. Explicitly attached browser identities SHALL be excluded.

#### Scenario: Personal Chrome remains running

- **WHEN** personal Chrome has no task-owned Playwright profile and managed task cwd
- **THEN** cleanup SHALL send it no signal, including when its arguments mention a worktree

#### Scenario: PID reused or ownership changed

- **WHEN** live process start identity, argv or cwd differs from a captured identity
- **THEN** neither TERM nor KILL SHALL be sent to that PID

#### Scenario: Selected bundle recognized

- **WHEN** a verified owned browser uses the selected bundle's macOS or Linux executable/helper names
- **THEN** cleanup recognizes it while rejecting a process that merely mentions that executable in its arguments

### Requirement: Full session and worktree cleanup includes browsers

Full session stops, natural agent exit, replacement restarts and worktree removal SHALL close verified owned browser resources for the ending generation. Managed resources SHALL receive graceful closure first, with captured-process SIGTERM then SIGKILL after five seconds as fallback. Agent-only/rerender stops SHALL preserve active surviving browser owners; transport termination SHALL close only the terminated adapter's resources. Escalation SHALL act only on the original snapshot, reverify identity, and respect cancellation and an optional eligibility recheck. Worktree removal SHALL capture ownership before deleting the worktree. Daemon restart alone SHALL NOT close active supervisor-owned browsers.

#### Scenario: Browser escaped the session group

- **WHEN** a task session is fully stopped and its browser has a separate process group
- **THEN** the browser and captured helpers SHALL receive cleanup independently of the agent group

#### Scenario: Natural exit leaves an MCP child

- **WHEN** an agent exits naturally while its owned browser remains alive
- **THEN** cleanup closes the ending generation's browser without waiting for task completion status

#### Scenario: Restart races old cleanup

- **WHEN** a replacement generation opens a browser during an earlier generation's cleanup grace period
- **THEN** old cleanup SHALL NOT signal the new browser

#### Scenario: Daemon bounce with active browser work

- **WHEN** the daemon restarts while the supervisor and browser operation remain alive
- **THEN** active owned browser resources survive and lifecycle tracking remains with their owner

### Requirement: Finished and missing task browsers are swept safely

The daemon SHALL scan every ten seconds and stop browsers after six eligible ticks when every worktree claimant is finished or missing and none has a live session. Task-list, running-list or whole process-scan failure SHALL prevent cleanup for that tick and reset its grace evidence. Candidate-local disappearance or ownership-read failure SHALL skip that candidate and invalidate its evidence without blocking independent verified candidates or resetting their grace counters. Eligibility SHALL be rechecked before TERM and KILL. Live unmanaged browsers SHALL remain protected; managed job inactivity is governed by operation tracking rather than this orphan sweep.

#### Scenario: Live or resumed task browser survives

- **WHEN** a finished task has a live session or another unfinished task claims the worktree
- **THEN** the orphan sweep SHALL preserve its browser

#### Scenario: Deleted task left a browser

- **WHEN** a verified task browser has no task row or live claimant for six consecutive eligible ticks
- **THEN** the daemon SHALL clean it up despite the missing row

#### Scenario: One candidate vanishes

- **WHEN** one PID vanishes during cwd inspection and another verified orphan remains eligible
- **THEN** the vanished PID is skipped and the independent orphan continues toward cleanup

### Requirement: Doctor reports task browser orphans without mutation

Doctor SHALL list task-owned browsers whose parent is PID 1, distinguish unavailable or partial inspection from a complete scan with no findings, and send no signals.

#### Scenario: Process inspection unavailable

- **WHEN** the process scan fails
- **THEN** doctor SHALL report UNKNOWN without changing its exit-code policy

#### Scenario: Partial inspection

- **WHEN** one candidate cannot be inspected but another verified orphan is found
- **THEN** doctor SHALL report the finding and incomplete inspection without claiming there are no other orphans
