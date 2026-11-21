## MODIFIED Requirements

### Requirement: Durable coordinator-owned board

The system SHALL store stable numeric-id items owned by coordinator role, with decision/fyi/action/todo kind, one-line subject, body, created/updated timestamps, read state, resolved state, resolver and optional resolution note. Only a coordinator binding SHALL authorize agent post/update/resolve/list tools. Material edits SHALL mark unread; identical edits SHALL preserve read state.

#### Scenario: Material edit and recycle

- **WHEN** a coordinator changes a read item's body and its session recycles
- **THEN** the same role retains the same unread item id and the fresh coordinator sees open ids and subjects in join, status and recycle context

#### Scenario: Ownership enforcement

- **WHEN** a worker or another coordinator attempts to mutate an item
- **THEN** the operation is rejected without changing the item

#### Scenario: Edit races a read acknowledgment

- **WHEN** the agent materially edits an item after Aaron expands its prior revision but before the asynchronous read acknowledgment commits
- **THEN** the edited item remains unread

### Requirement: Human-directed board capture

The system SHALL ship a builtin `hera-remind-me` skill that Aaron invokes to have the current agent put an item from the conversation on its coordinator's board. With argument text the agent SHALL capture what the text names; without it the agent SHALL capture the most recent actionable follow-up from the conversation. The item SHALL have a one-line subject and a body that states the action, its trigger condition, its rationale and any concrete references. Its kind SHALL be `todo` for future team work, `action` with blocking false for a deferred step Aaron must perform, `action` with blocking true when Aaron must act to unblock work now, `decision` for a human choice, and `fyi` for information only. Trigger text, pending readiness and any exact human step SHALL use the corresponding typed fields. The board kind `todo` SHALL remain separate from the Things 3 backend.

A coordinator SHALL list its open items first and update an existing item that already covers the same follow-up instead of posting a duplicate. A worker or freelance agent SHALL NOT call board tools; it SHALL relay the item to its coordinator with `hera_send` and ask the coordinator to post it. An agent with no Hera binding SHALL report that no board exists in this session and SHALL NOT post or send anything. The skill SHALL NOT change board ownership or authorization.

#### Scenario: Coordinator captures a release follow-up

- **WHEN** a coordinator has just said "when we release, bump the service worker version" and Aaron runs `/hera-remind-me`
- **THEN** a new open `todo` item exists on that coordinator's board whose subject names the follow-up and whose body includes the release trigger and the action

#### Scenario: Existing item covers the follow-up

- **WHEN** the coordinator's board already has an open item for the same follow-up and Aaron runs `/hera-remind-me` for it
- **THEN** the existing item is updated with the newer information and no second item is posted

#### Scenario: Worker relays to its coordinator

- **WHEN** Aaron runs `/hera-remind-me` in a worker session
- **THEN** the worker sends its coordinator a Hera message containing the proposed subject, kind, metadata and body with a request to post it, and makes no board tool call

#### Scenario: Plain task has no board

- **WHEN** Aaron runs `/hera-remind-me` in a task with no Hera binding
- **THEN** the agent says there is no board for this session and nothing is posted or sent

## ADDED Requirements

### Requirement: Typed board action and todo metadata

Board items SHALL accept kind `action` for a step only Aaron can perform and kind `todo` for future work the team performs. Decisions and FYIs SHALL retain their behavior. Top-level fields SHALL be `blocking` (boolean), `trigger` (text up to 1024 UTF-8 bytes), `trigger_pending` (boolean) and `command` (exact copyable text up to 16 KiB). Blocking and command SHALL be action-only; trigger and trigger_pending SHALL be action/todo-only. Decision structure SHALL remain decision-only. Action posts SHALL explicitly specify blocking. Pending triggers SHALL require nonempty trigger text and SHALL NOT be blocking. Empty strings SHALL clear optional text; nonempty whitespace-only text SHALL be invalid. Omitted update fields SHALL preserve values, except values incompatible with a changed kind SHALL clear. A trigger on post SHALL default to pending unless readiness is explicitly supplied; without a trigger it SHALL default ready. Material metadata edits SHALL mark unread and identical edits SHALL preserve read state. Agent post/update/list tools SHALL advertise and decode all fields consistently, list SHALL accept an optional four-kind filter, and REST SHALL expose the same stored metadata. Only existing coordinator authorization SHALL permit agent mutations.

#### Scenario: Blocking command

- **WHEN** a coordinator posts an action with blocking true and an exact command
- **THEN** the stored/listed item retains the exact command as data and no process executes it

#### Scenario: Deferred trigger

- **WHEN** a coordinator posts a todo with trigger “after 1.42 ships” and no readiness override
- **THEN** it is stored pending until the coordinator explicitly updates trigger_pending to false

#### Scenario: Invalid combinations and safe updates

- **WHEN** a coordinator supplies blocking on a todo, a pending blocking action or decision options on an action
- **THEN** the request is invalid without writing; valid omitted metadata updates preserve existing values and changing kind clears incompatible values

#### Scenario: Migration preserves dependent data

- **WHEN** an existing database upgrades to support all four kinds
- **THEN** IDs, existing row values, decision structures, approvals and response receipts remain intact, metadata has neutral defaults, foreign keys remain enforced and reopening is idempotent

### Requirement: Quiet deferred work and blocking priority

The board SHALL order unresolved blocking actions first, existing decisions/FYIs and ready nonblocking actions second, and pending items plus all board todos last, using ascending id within each band. Existing decisions/FYIs SHALL keep their relative order. Pending-trigger items and every board todo SHALL remain visible/listable with their trigger/readiness but SHALL NOT contribute to human-attention unread counts, mailbox badges, automatic board landing or needs-input jumps. Coordinator recovery SHALL include their ids, kinds and readiness. Coordinators SHALL verify triggers explicitly; the system SHALL NOT infer readiness from prose or automatically schedule work. Ready actions SHALL retain ordinary unread behavior.

#### Scenario: Blocking beats freshness and read state

- **WHEN** a read blocking action and a newer unread decision exist
- **THEN** the blocking action is displayed first while the decision retains its existing unread behavior

#### Scenario: Quiet future work

- **WHEN** the only unread items are pending actions and board todos
- **THEN** all items stay visible but the coordinator has no human-attention unread badge or board-only jump target

#### Scenario: Human step becomes urgent

- **WHEN** a coordinator verifies a deferred action's trigger and updates it to trigger_pending false and blocking true
- **THEN** the material edit is unread, contributes to attention and sorts first

### Requirement: Durable action and board todo completion

An action SHALL resolve as `done` with optional note. A board todo SHALL resolve as `done` or `dropped` with optional note. Resolution SHALL store outcome in resolution_choice, resolver and note, atomically enqueue a durable coordinator notice and publish an event only after commit. Human operations SHALL require expected revision, use idempotent action receipts and reject incompatible outcomes/kinds. Agent resolution SHALL default to done for these kinds and accept an explicit dropped outcome only for board todos. Read/acknowledgment SHALL NOT complete either kind, and no UI SHALL execute commands. Existing decision/FYI resolution and notice semantics SHALL remain unchanged.

#### Scenario: Human completion survives delivery loss

- **WHEN** Aaron marks an action done, the transaction commits and the HTTP response or live notice delivery is lost
- **THEN** the optional note and done outcome persist, retry replays the same receipt and exactly one notice remains deliverable

#### Scenario: Team work dropped

- **WHEN** Aaron or its coordinator resolves a board todo as dropped
- **THEN** it resolves with dropped outcome and its notice distinguishes dropped from done

#### Scenario: Invalid or stale completion

- **WHEN** Aaron attempts dropped on an action or completes a stale revision
- **THEN** the operation returns invalid or conflict without changing the item or sending a completion notice

### Requirement: Action and board todo controls

The TUI and SPA board SHALL label all four kinds and show action blocking state, trigger/readiness and exact copyable step. The TUI SHALL provide a rebindable one-key Done (default d) for action/todo and Dropped (existing x) for board todo; x SHALL NOT silently dismiss an action. A copy action SHALL copy the exact command/step. Optional completion notes SHALL be available in the browser completion UI. Keymap help and Reference documentation SHALL describe the new controls. Coordinator orientation, board tools and builtin Hera/remind-me guidance SHALL distinguish human steps, future team work, choices and FYIs, including explicit trigger updates and the Things naming distinction. New operations SHALL log success, failure and skipped/stale work without command contents.

#### Scenario: One-key done

- **WHEN** Aaron presses d on the selected action or board todo
- **THEN** the exact visible revision resolves done without requiring a note and the coordinator receives its durable completion notice

#### Scenario: Copy human step

- **WHEN** Aaron invokes Copy on an action's command
- **THEN** the exact stored text is copied without execution

#### Scenario: Guidance classifies reminders

- **WHEN** a coordinator captures future team work or a deferred command Aaron must run
- **THEN** guidance selects board todo for the former and deferred action for the latter, with typed trigger data
