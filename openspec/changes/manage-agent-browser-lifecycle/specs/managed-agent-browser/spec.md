## ADDED Requirements

### Requirement: Owned browser launches use a pinned bundled engine

Sandboxed Claude and Codex managed browser jobs SHALL share a resolved policy with exact MCP, Playwright and matching browser revision. Default launches SHALL use headless bundled Chromium, an isolated context and no branded channel. Missing or invalid bundles SHALL fail with an actionable diagnostic without falling back to installed Google Chrome. The nested browser sandbox SHALL be disabled when running inside Argus's outer sandbox; unsandboxed launches SHALL not inherit that exception automatically.

#### Scenario: Default launch on macOS

- **WHEN** either supported backend starts a managed browser job with no mode override
- **THEN** the launch uses the pinned headless shell and never `/Applications/Google Chrome.app`

#### Scenario: Required runtime is absent

- **WHEN** the configured browser revision cannot be verified
- **THEN** the job fails before browser launch and identifies the required installation

### Requirement: Visible automation is an explicit finite choice

Managed jobs SHALL offer an explicit per-job headed option using the pinned full bundled Chromium build. Default errors SHALL NOT trigger an automatic headed retry. Owned headed browsers SHALL use the same finite inactivity policy as headless browsers.

#### Scenario: Visible debugging requested

- **WHEN** the user requests visible browser debugging and the agent starts an explicitly headed job
- **THEN** bundled Chromium opens visibly and closes on job completion or eligible inactivity

### Requirement: Browser jobs close owned resources

The managed MCP adapter SHALL expose explicit job completion and support cancellation and transport shutdown. The direct runner SHALL close owned contexts in a finally path on success, exception or cancellation. A completed job SHALL close its owned browser resources without stopping the agent; subsequent jobs SHALL receive fresh isolated contexts. Closing SHALL be idempotent and generation-scoped.

#### Scenario: Sequential jobs

- **WHEN** one job finishes and the same agent starts a second job
- **THEN** the first job's context and owned browser are closed and the second job has a fresh context

#### Scenario: Cancelled work

- **WHEN** a job is cancelled during an operation
- **THEN** cancellation aborts/unwinds that job within a bounded deadline and closes only its owned resources

### Requirement: Inactivity is measured from browser operations

Owned browsers SHALL close after a validated positive timeout, proposed default 300 seconds and supported range 60–3600 seconds, measured from the last settled browser operation. In-flight operations SHALL suspend idle closing. Timer expiry, new operations and closure SHALL serialize so resumed activity cannot race a stale timeout. Review status, terminal output and protocol keepalives SHALL NOT establish or renew browser activity. Explicit renewal SHALL be finite. A subsequent request SHALL reopen lazily and report expired state.

#### Scenario: Idle in-review agent

- **WHEN** a live agent in review has no browser operations for the configured timeout
- **THEN** its owned headed browser closes while the agent remains available

#### Scenario: Long operation and resumed work

- **WHEN** an operation remains in flight beyond the idle timeout or a new operation wins the expiry race
- **THEN** idle cleanup does not close its context

### Requirement: Attached browsers are excluded from ownership cleanup

Personal Chrome, Claude-in-Chrome, extension connections and explicitly attached browsers SHALL NOT receive idle-close timers or process signals from managed browser cleanup. Releasing a connection SHALL NOT close the user's browser or pre-existing tabs. Shared caches and task status SHALL NOT establish ownership.

#### Scenario: Attached session ends

- **WHEN** an attached browser job finishes, times out, or its agent stops
- **THEN** cleanup releases only its connection and preserves the personal browser and user tabs

### Requirement: Launch and lifecycle events are attributable and redacted

Local telemetry SHALL identify task, generation/job, engine/build, mode, ownership, owner/browser process identity, lifecycle outcome and enumerated reason. It SHALL cover failed launches, closure, exit and skipped cleanup. It SHALL NOT log raw argv/environment, URLs, credentials, browser content, cookies, script arguments or unfiltered upstream errors. Deployment verification SHALL independently identify running daemon and supervisor builds and the managed runtime revision.

#### Scenario: Failed launch contains a sensitive upstream error

- **WHEN** launching a task browser fails with an error containing a URL or credential
- **THEN** the recorded event identifies the task, engine and sanitized error class without recording the sensitive text

#### Scenario: Supervisor still runs an older build

- **WHEN** the on-disk binary and daemon have changed but the supervisor has not
- **THEN** verification reports the skew and does not claim the browser policy is fully deployed
