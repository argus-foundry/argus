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

An idempotency key plus the same command arguments SHALL return the original job and SHALL NOT run the operation again. The same key with different arguments SHALL be rejected. A confirm refusal SHALL NOT be stored, so a later confirmed call MAY reuse the key.

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
