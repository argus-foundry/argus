# Voice Control

## Purpose

Voice control lets a speakable MCP client drive Argus without a worktree directory. Short named commands reuse the existing task, session, and status paths. Answers meant to be read aloud are plain sentences. An optional loopback webhook reports when a session needs input or a task completes.

## Requirements

### Requirement: Voice commands are an allowlist without a working directory

The system SHALL expose a named-command registry through the MCP server. Each command SHALL declare the arguments it reads. A call SHALL NOT require a working directory. The registry SHALL NOT include `kb_ingest` or `kb_delete`, and a request for either name SHALL be refused without changing the knowledge base. Unknown command names SHALL be refused.

#### Scenario: Knowledge-base writes are refused

- **WHEN** a voice command names `kb_ingest` or `kb_delete`
- **THEN** the call is refused, the speech says that command is not available by voice, and no document is written or deleted

#### Scenario: A workflow is addressed by name or task id

- **WHEN** a voice command names a task id, or a workflow name that matches one non-archived task
- **THEN** the command targets that task without a working directory

#### Scenario: A repeated name is not guessed

- **WHEN** a voice command names a workflow that matches more than one project and no project is given
- **THEN** the call is refused and the speech asks for the project

### Requirement: Spoken brief reuses status counts

The system SHALL provide a brief whose speech is one to three plain sentences covering running work, idle work, work that needs input, blocked work, and work finished within the last half hour. The brief SHALL also return structured counts. The session running and idle totals and the task status counters SHALL match `GET /api/status`. Speech SHALL NOT contain markdown.

#### Scenario: An empty fleet

- **WHEN** a brief is requested and nothing is running, idle, blocked, needing input, or recently finished
- **THEN** the speech says nothing is running and nothing finished in the last half hour

#### Scenario: Needs input is spoken by name

- **WHEN** one in-progress session needs input
- **THEN** the speech names that task and says it needs input, and the counts include that need

### Requirement: Workflow commands return a job and a spoken confirmation

Starting, stopping, resuming, and sending input SHALL reuse the existing task create, stop, resume, and session-input paths. Each accepted call SHALL return a job handle and a one-sentence confirmation without waiting for a long-running start to finish. `wait` and `status` SHALL report that job. `wait` SHALL accept a timeout and SHALL NOT block longer than three seconds. When the job is unfinished at the deadline, the speech SHALL be `Still running.` Sending input SHALL require a task id. Stop SHALL NOT run unless confirm is true. Resuming a closed-out workflow SHALL NOT force revival unless confirm or force is set.

The session owner SHALL arbitrate a confirmed stop against pending starts and restarts before selecting the stop target. During a launch, the call SHALL either wait for a usable session or report that it cannot stop the session yet.

#### Scenario: Stop without confirm does nothing

- **WHEN** stop is requested without confirm
- **THEN** the session is not stopped and the speech asks for confirm

#### Scenario: A short wait does not block

- **WHEN** wait is called for a job that is still running and the timeout is longer than the cap
- **THEN** the call returns within the cap and the speech is `Still running.`

#### Scenario: Start of a new workflow needs a project and a prompt

- **WHEN** start names a workflow that does not exist and omits the project or the prompt
- **THEN** no task is created and the speech asks for both

#### Scenario: Send targets a task id

- **WHEN** send is requested with a name and no task id
- **THEN** nothing is written to a session and the speech says send needs a task id

### Requirement: Idempotency keys replay the same job

While a job is retained in the daemon's bounded 256-job store, its idempotency key plus the same command arguments SHALL return the original job without running the operation again. The same key with different arguments SHALL be rejected while the job is retained. Finished jobs MAY be evicted to admit new jobs. A confirm refusal SHALL NOT be stored, so a later confirmed call MAY reuse the key.

#### Scenario: A retried start does not create a second task

- **WHEN** start is called twice with the same idempotency key, name, project, and prompt
- **THEN** both calls report the same job and only one task is created

#### Scenario: A key cannot change meaning

- **WHEN** a second call reuses an idempotency key with a different command or different arguments
- **THEN** the call is rejected and the original job is unchanged

### Requirement: Loopback webhook for needs-input and completion

The system SHALL accept an optional webhook URL in configuration. An empty URL SHALL disable delivery. A non-empty URL SHALL be used only when it is `http` or `https` and the host is localhost or a loopback address. Delivery SHALL be best-effort, SHALL NOT follow redirects, and SHALL NOT block event emission. The system SHALL POST a JSON body whose speech is one plain sentence when a session enters needs-input and when a task completes. A needs-input event whose payload says the session does not need input SHALL NOT be posted.

#### Scenario: Needs input posts one sentence

- **WHEN** `session.needs_input` is emitted with `needs_input` true and a loopback webhook URL is configured
- **THEN** the URL receives one POST whose speech says the task needs input

#### Scenario: Clearing needs input does not post

- **WHEN** `session.needs_input` is emitted with `needs_input` false
- **THEN** the webhook receives no POST

#### Scenario: A public URL is not called

- **WHEN** the configured webhook URL is not a loopback host
- **THEN** no request is sent to that host

#### Scenario: An empty URL disables delivery

- **WHEN** no webhook URL is configured and a task completes
- **THEN** no webhook request is sent

### Requirement: Voice mutations require the API master token

The MCP server SHALL require the Argus API master token in an Authorization bearer header for voice start, stop, resume, and send. These commands SHALL require a loopback HTTP host and, if present, a matching HTTP origin. A refused call SHALL NOT create or change a job or task. The credential SHALL NOT be accepted as a tool argument or query parameter.

#### Scenario: A voice client sends input without a credential

- **WHEN** a voice client calls send without the API master token in the bearer header
- **THEN** the call is refused and no session input is written

### Requirement: Launch finalization preserves a concurrent stop

Successful REST and voice stops SHALL supersede overlapping launch finalization, including when the task was already in review. REST stop SHALL return a conflict for a Pending task without a session and preserve its status. The TUI SHALL attach to an existing session after create or fork and leave a concurrent stop in effect. Launch persistence SHALL acquire SQLite write intent before reading the state that it will update.

#### Scenario: REST stop before the PTY exists

- **WHEN** REST stop targets a Pending task before its first session is available
- **THEN** the call returns a conflict and the task stays Pending so normal launch finalization can proceed

#### Scenario: Stop supersedes native resume from in review

- **WHEN** a voice stop terminates the PTY while the native TUI is completing a resume
- **THEN** launch finalization preserves the stopped task state even when it was already in review before resume

### Requirement: Concurrent session lifecycle operations

Native TUI and daemon stop and resume operations for a task SHALL be mutually exclusive through persisted finalization. REST stop SHALL refuse while the session owner has reserved a start or queued a restart. A deferred native restart SHALL be discarded when a confirmed stop has advanced its recorded stop generation.

#### Scenario: An outgoing session stops while the user resumes the task

- **WHEN** a native TUI resume is attempted while the daemon has signaled the outgoing session and is still persisting the stop
- **THEN** the TUI leaves the task alone and the user can resume after the stop operation completes

#### Scenario: A scheduled native restart overlaps a confirmed stop

- **WHEN** a successful voice or REST stop advances the stop generation before a queued native rerender resumes
- **THEN** the deferred rerender leaves the task stopped

#### Scenario: Another resume already launched the task

- **WHEN** the native TUI attempts a resume after the daemon has launched a session for the same task
- **THEN** the TUI leaves the existing session and its persisted state intact

### Requirement: Exit notifications retain session ownership

A daemon-backed TUI SHALL leave persisted task status and session identity to the daemon. A notification from a retired session SHALL NOT detach a live replacement or alter its task state. The standalone TUI SHALL keep persisting its own session exits.

#### Scenario: An outgoing exit notification arrives after a resume

- **WHEN** a daemon-backed TUI receives a delayed notification for a previously stopped session after a replacement has started
- **THEN** the replacement stays connected and its persisted task remains in progress

### Requirement: Owner-managed remote TUI rerender preserves width decisions

When the remote TUI requests a rerender, the daemon SHALL consider the committed width for the current process and allow a deliberate same-width retry. The daemon SHALL still verify the process, transcript availability, and the normal safety conditions before queuing a restart.

#### Scenario: A panel returns to the initial width after repair was deferred

- **WHEN** the panel returns to the process's initial width but differs sufficiently from the committed history width for that process
- **THEN** the daemon evaluates a rerender at the panel width

#### Scenario: An explicit rerender uses a previously requested width

- **WHEN** the user requests a rerender at a width already sent to the daemon
- **THEN** the daemon evaluates the request again using the current process and the normal safety checks

### Requirement: Resume waits for stopped sessions to retire

A resume SHALL refuse to reattach to a session that has already received a stop. Native TUI repair SHALL refuse to schedule a restart against that process, including for resident tasks whose status stays in progress. The same task MAY be resumed once the session owner retires the stopped process.

#### Scenario: A task is resumed while its process is terminating

- **WHEN** the session owner is still retiring a previously stopped process
- **THEN** REST and voice report a restart conflict and the native TUI leaves the stopped state intact

### Requirement: Relayed exits apply to the retired process

The daemon SHALL verify that a relayed supervisor exit belongs to the retiring process before changing task state and streams. Finalization of the retiring process and persistence of an explicit replacement SHALL be coordinated so the old exit cannot alter the replacement.

#### Scenario: A supervisor exit arrives after a replacement starts

- **WHEN** a delayed supervisor exit for an outgoing process reaches the daemon after a newer process for the task is active
- **THEN** the newer process remains active and keeps its task status and streams

#### Scenario: The replacement stream starts before the old stream finishes

- **WHEN** the client attaches a newly launched process before the earlier connection is cleaned up
- **THEN** it uses a fresh stream and output cursor, and cleanup of the retired connection preserves the new handle

### Requirement: Process-scoped width repair never affects a replacement

The owner SHALL verify the intended process before applying both resize and rerender operations requested with a process ID. Native repair SHALL refuse if the observed process is retiring or has been replaced.

#### Scenario: A replacement starts before a scoped width request reaches the owner

- **WHEN** the request's process ID no longer matches the live owner
- **THEN** the owner neither resizes nor restarts the replacement

### Requirement: Connected Hera reclaim preserves terminal completion

When the native TUI deliberately reclaims and archives a live Hera task, its terminal intent SHALL reach the daemon and apply to the confirmed exit of that session only.

#### Scenario: A connected operator reclaims a live task

- **WHEN** the task's reclaimed session exits
- **THEN** the archived task reaches Complete unless its active Hera worker binding requires review
