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

### Requirement: Managed restarts report every terminal outcome

A successfully adopted managed replacement SHALL receive a fresh stream retry allowance. If a managed replacement exits before adoption, the daemon SHALL receive its confirmed exit and finalize that process's task lifecycle.

#### Scenario: A terminal follows several managed restarts

- **WHEN** a bound terminal follows more normal managed restarts than its transport retry count
- **THEN** it remains attached to the current process

#### Scenario: The replacement exits before its stream can attach

- **WHEN** a managed replacement starts and exits during the attach retry interval
- **THEN** the daemon applies that replacement's confirmed task exit

### Requirement: Scoped output has no replay gap

HTTP output SHALL join the same process's replay and live bytes without omitting output between replay and subscription.

#### Scenario: Output arrives during a replay

- **WHEN** the PTY publishes bytes while a process-scoped HTTP stream attaches
- **THEN** the client's replay and live stream collectively include the bytes in order

### Requirement: Reclaim stops the certified process only

Connected Hera reclaim SHALL require the owner to stop the certified process. Worktree removal SHALL wait until there is no active process or the owner accepts the stop for that process.

#### Scenario: The reclaim target changes before the stop

- **WHEN** the session owner reports a replacement rather than accepting the certified reclaim PID
- **THEN** the replacement and worktree remain available

#### Scenario: The reclaim exit has yet to be finalized

- **WHEN** a reclaim sweep sees an empty process slot while the task still has durable reclaim intent
- **THEN** it leaves the task and worktree available until the daemon settles the exit

### Requirement: Reclaim recovers the retiring process

After daemon restart or lost delivery, the daemon SHALL recover a retained confirmed exit for an armed reclaim PID. It SHALL preserve the worktree while the process or its exit remains unverified. Reclaim SHALL verify that no live Hera binding remains before acting. PID-scoped stops SHALL signal only the approved process tree.

#### Scenario: The daemon misses a confirmed reclaim exit

- **WHEN** a daemon restarts after the supervisor observes the armed process exit
- **THEN** reconciliation finalizes that exact exit and reclaims the eligible task

### Requirement: Owner-managed restart remains observable

An owner that cannot report the managed successor PID SHALL refuse newly requested managed restarts. A confirmed restart that is still in owner prelaunch SHALL remain tracked independently of retries for a dropped transport.

#### Scenario: A managed restart spends a long time in prelaunch

- **WHEN** the reported pending restart outlasts the reconnect budget
- **THEN** the client still receives the owner's final result

### Requirement: Terminal cursors stay with their process

A live terminal snapshot SHALL identify its process alongside the cursor. Clients SHALL pass that identity when attaching the live stream and refresh their snapshot if the process changed.

#### Scenario: A session changes between snapshot and attachment

- **WHEN** the current process differs from the one associated with the requested output cursor
- **THEN** the client opens a fresh snapshot without reusing the outgoing cursor

### Requirement: Scoped lifecycle recovery

Terminal reconnects SHALL retain the requested process identity through stream attachment. Native clients SHALL retry temporary snapshot conflicts during replacement.

#### Scenario: Process changes during attachment
- **WHEN** a terminal reconnect requests an offset for the old process and a replacement is installed before attachment
- **THEN** the request receives a stale-session conflict and cannot apply the old offset to the replacement.

#### Scenario: Previous supervisor cannot manage a live backend move
- **WHEN** a live Claude or Codex task is moved between backends and the session owner cannot manage its restart
- **THEN** the move fails before persisting its new backend or handoff.

#### Scenario: Hera stop delivery is interrupted
- **WHEN** the daemon finds a durable reclaim intent for an archived, unbound task whose exact process is still live
- **THEN** it retries the PID-scoped stop and waits for a matching exit before reclaiming.

#### Scenario: Hera role admission overlaps reclaim
- **WHEN** a task is being stopped or its worktree is being removed by Hera reclaim
- **THEN** a new live binding for that task cannot commit during reclaim.

#### Scenario: Daemon reattaches while a launch is reserved
- **WHEN** the owner has reserved a launch or a managed successor is still starting
- **THEN** owner listings retain the reservation and the new daemon keeps tracking the launch until the owner settles it.

### Requirement: Startup terminal and reclaim handoffs

Startup reattachment SHALL use authoritative owner observations to settle pending managed launches. Terminal clients SHALL preserve process identity through reconnect. Rebasing an archived task into active use SHALL coordinate with Hera reclaim.

#### Scenario: A launch settles during startup attachment
- **WHEN** a daemon sees an owner-managed launch or a live task during startup and that process settles before the stream attaches
- **THEN** a retained matching terminal receipt is finalized without restoring that task as a live worker.

#### Scenario: Restore races Hera reclaim
- **WHEN** a task is being stopped or its worktree is being reclaimed
- **THEN** restore, pin, and new role binding cannot race that operation.

#### Scenario: Theia terminal resumes across a replacement
- **WHEN** the Theia terminal sees a different process ID or a stale snapshot
- **THEN** it refreshes terminal state and uses only the replacement process’s cursor.

#### Scenario: Remote TUI discovers a replacement stream
- **WHEN** a remote TUI reader receives a stale-session conflict or a rerendering exit
- **THEN** it listens for the replacement with a fresh cursor and the pane reattaches without a task exit callback.

### Requirement: Outgoing identity through terminal handoff

A queued managed restart SHALL report the outgoing process identity before the process stops. Recorded terminal history SHALL refresh from the current snapshot when the producing process cannot be identified.

#### Scenario: Daemon joins before outgoing exit receipt
- **WHEN** a daemon observes a queued restart while the outgoing process is retired and its exit callback has yet to publish a receipt
- **THEN** the observation identifies the outgoing PID and the relay waits for its scoped receipt.

#### Scenario: Recorded history changes while Theia is suspended
- **WHEN** Theia resumes a cached terminal and the new snapshot contains recorded output with no process ID
- **THEN** it replaces the displayed history with that snapshot without using its byte count as a live stream cursor.

#### Scenario: Relaunch races a Hera reclaim stop
- **WHEN** an archived Hera task still has an armed stop for its retiring process
- **THEN** restart and resume are refused until that stop settles, regardless of the task's workflow status.

#### Scenario: Nuke is requested through REST or standalone TUI
- **WHEN** the final role is nuked
- **THEN** binding admission is fenced through the scoped stop and the worktree is removed only after owner retirement.

#### Scenario: An SSE client sees a pending relay
- **WHEN** an attachment sees an owner relay without a PID or a remote terminal receives a stale snapshot conflict during replacement
- **THEN** it cannot consume output with an unscoped cursor, and the terminal waits for settled owner state before reconnecting or declaring exit.

### Requirement: Terminal observation and reclaim completion

Owner observations SHALL pair the process slot with its last confirmed exit. Recovery SHALL preserve owner identity through delayed exit delivery. Hera SHALL retain reclaim ownership until terminal retirement is settled.

#### Scenario: An ordinary reserved process retires before attachment
- **WHEN** a consumer observes a launch reservation and the process exits before attachment
- **THEN** the consumer waits for the process's retained exact exit before settling it.

#### Scenario: PWA sees stale snapshots during replacement
- **WHEN** a snapshot remains stale while a replacement has yet to settle
- **THEN** the terminal stays in a single bounded recovery loop.

#### Scenario: Hera's receipt payload expires
- **WHEN** no live binding exists and the owner confirms both retirement and the exact armed PID as its most recent exit
- **THEN** reclaim clears its intent without fabricating a terminal event or exit code.

#### Scenario: Nuke arrives during managed prelaunch
- **WHEN** the archive and role transition commit while an active launch temporarily prevents a scoped stop
- **THEN** the daemon retries the stop after the owner settles, and generic relaunch and pruning wait for reclaim.

### Requirement: Recovery owns its operation through completion

PWA terminal recovery SHALL act only on the current attachment and SHALL enforce its deadline while waiting for a snapshot. Hera reclaim SHALL recover contended operations without bypassing resource ownership.

#### Scenario: An old terminal recovery loses ownership
- **WHEN** a newer attachment replaces a recovering terminal view
- **THEN** the old operation cannot mutate or restart the newer view.

#### Scenario: Recovery snapshot stalls
- **WHEN** snapshot response or rendering does not finish before the recovery deadline
- **THEN** the recovery is canceled and its queued input is settled.

#### Scenario: Initial nuke archive contends or bulk delete races
- **WHEN** nuke has ended the last binding and archive could not acquire the owner lock
- **THEN** the daemon retries archival and scoped stop, while generic bulk deletion skips any task that reclaim owns before resource side effects.

#### Scenario: Standalone nuke finishes without a worktree
- **WHEN** the standalone owner is retired and branch cleanup has finished
- **THEN** ownership is released while the archived task stays available to normal task actions.

#### Scenario: Retirement receipt lookup fails
- **WHEN** owner retirement is confirmed but the receipt lookup fails temporarily
- **THEN** reclaim retries the lookup and does not treat the transport failure as record expiration.

### Requirement: Nuke and terminal admission

An ended last binding from a committed role nuke SHALL fence generic operations before archival. Nuke refusals SHALL be visible and keep any unsettled roles reachable. Terminal recovery SHALL remain bounded until snapshot rendering and stream attachment finish.

#### Scenario: Role nuke commits while archive is deferred
- **WHEN** another lifecycle operation temporarily prevents archive
- **THEN** generic operations preserve the unarchived task and periodic recovery verifies its unsettled nuke under lock.

#### Scenario: A cascade cannot mutate a role
- **WHEN** a role mutation is refused
- **THEN** the operator is told and the orchestrator remains reachable.

#### Scenario: Standalone cleanup retained the archived task
- **WHEN** an ordinary archive or metadata update follows completed reclaim
- **THEN** it cannot re-arm the already settled binding.

#### Scenario: Recovery loses its view or cannot complete rendering and attachment
- **WHEN** the view closes, its snapshot rendering stalls, or the replacement SSE stream refuses authentication or attachment
- **THEN** outdated work cannot open another stream and retries remain within the original recovery deadline.

### Requirement: Committed reclaim evidence and terminal teardown

Reclaim SHALL distinguish a committed cleanup from a role nuke that preserved a shared task. A deferred operation SHALL verify its binding before changing a task. Terminal view actions SHALL use the initiating view through completion.

#### Scenario: A preserved binding changes later
- **WHEN** a role is nuked while its task remains live-bound elsewhere
- **THEN** a later ordinary rebind or detach does not turn that older nuke into a reclaim request.

#### Scenario: A pending cleanup has already settled
- **WHEN** a delayed TUI reclaim callback picks up after the binding was settled
- **THEN** it leaves that retained task's current session and archive state alone.

#### Scenario: Generic mutation meets active reclaim
- **WHEN** generic archive, relaunch, or deletion meets a reclaim-owned task
- **THEN** it cannot change that task. Clear archive reports any role it could not nuke.

#### Scenario: Terminal session loses its view
- **WHEN** the user logs out or a resume request outlives its initiating detail view
- **THEN** no obsolete renderer or stream attaches.

### Requirement: Recovery ownership across background transitions

Hera reclaim SHALL establish that a role nuke reserved the task before reclaiming its resources. A pending graduation SHALL prevent a nuke of its associated role. PWA background refresh SHALL preserve an active terminal recovery deadline, and reauthentication SHALL preserve the selected tab.

#### Scenario: Legacy deletion preserved another binding
- **WHEN** a legacy role nuke predates another role's binding end or a later binding
- **THEN** ordinary rebind or detach does not make the legacy nuke a cleanup claim.

#### Scenario: A hidden role has pending graduation
- **WHEN** the latest ended binding identifies a task with a pending graduation
- **THEN** a nuke leaves the role and reclaim state unchanged.

#### Scenario: Resident cleanup lacks retirement confirmation
- **WHEN** resident cleanup has no current reclaim claim or the session owner cannot confirm retirement
- **THEN** it preserves the worktree and defers eligible cleanup until retirement is confirmed.

#### Scenario: A background refresh observes replacement is live
- **WHEN** ordinary PWA refresh or reconnect sees a replacement while terminal recovery is active
- **THEN** attachment remains subject to the existing recovery deadline.

#### Scenario: Logging out from another tab
- **WHEN** the PWA user logs out and signs in again on the same page
- **THEN** the selected tab remains the only tab displayed.

### Requirement: Delayed detail actions preserve the current visit

A response to a PWA task mutation SHALL update or replace a detail view only if that view is still the one that initiated the mutation.

#### Scenario: A fork returns after navigating elsewhere
- **WHEN** the user leaves a detail view before its fork request or subsequent task refresh completes
- **THEN** the fork does not replace the detail view the user chose.

### Requirement: Current ownership before cleanup or live attachment

Cleanup SHALL verify that a task still owns its local worktree or branch, and stacked cleanup SHALL honor a resident cleanup claim. Terminal clients SHALL keep ownerless recorded history separate from a replacement process. PWA mutation controls SHALL belong to one detail visit.

#### Scenario: A worktree is reused
- **WHEN** deferred cleanup encounters resources now owned by a different task
- **THEN** the local replacement is preserved, regardless of whether the earlier task is still present.

#### Scenario: A resident cleanup also belongs to a merged stack
- **WHEN** another cleanup candidate includes a branch with a resident cleanup claim
- **THEN** that candidate leaves the branch to the resident cleanup lease.

#### Scenario: Recorded output and a newly live process
- **WHEN** a terminal loads recorded output without a session PID and subsequently sees a live process
- **THEN** it obtains that process's snapshot before displaying live bytes.

#### Scenario: A blocked action outlives a detail visit
- **WHEN** the user leaves a detail while Resume or Delete is pending
- **THEN** the next visit starts with independent action controls, and its model can still refresh authoritative task names.

### Requirement: Reconcile an authoritative task deletion

A successful PWA delete SHALL close the currently displayed detail if it shows the deleted task, including a reopened visit. A different task's detail SHALL remain open.

#### Scenario: Reopening a task during Delete
- **WHEN** a delete finishes while the reopened detail still shows that deleted task
- **THEN** the detail closes. If the selected task has a different identity, its terminal stays open.

### Requirement: Reconcile authoritative task archival

After a successful Archive or Unarchive, the PWA SHALL close the detail for that task if it no longer belongs in the active list filter. A different task's detail SHALL remain open.

#### Scenario: An archive response returns after the same task is reopened
- **WHEN** success removes the task from the selected Active or Archived list
- **THEN** its reopened terminal closes, while an unrelated task's terminal stays open.

### Requirement: Reconcile a delayed archive response with current state

A delayed Archive or Unarchive completion SHALL use the task's authoritative current archival state before closing a newer detail visit.

#### Scenario: Unarchive succeeds before the older Archive response arrives
- **WHEN** the user has reopened the currently active task after Unarchive
- **THEN** the older Archive response leaves the active terminal open.
