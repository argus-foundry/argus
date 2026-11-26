## MODIFIED Requirements

### Requirement: Typed board action and todo metadata

Board items SHALL accept kind `action` for a step only Aaron can perform and kind `todo` for future work the team performs. Decisions and FYIs SHALL retain their behavior. Top-level fields SHALL be `blocking` (boolean), `trigger` (text up to 1024 UTF-8 bytes), `trigger_pending` (boolean) and `command` (exact copyable text up to 16 KiB). Blocking and command SHALL be action-only; trigger and trigger_pending SHALL be action/todo-only. Decision structure SHALL remain decision-only. Action posts SHALL explicitly specify blocking. Pending triggers SHALL require nonempty trigger text and SHALL NOT be blocking. Empty strings SHALL clear optional text; nonempty whitespace-only text SHALL be invalid. Omitted update fields SHALL preserve values, except values incompatible with a changed kind SHALL clear. A trigger on post SHALL default to pending unless readiness is explicitly supplied; without a trigger it SHALL default ready. Material metadata edits SHALL mark unread and identical edits SHALL preserve read state. A readiness-only trigger_pending update SHALL mark unread and emit the normal update event without advancing updated_at, because it does not change authored response content. Authored kind, subject, body, structure, blocking, trigger text and command changes SHALL advance the response revision. Read acknowledgment and listing/reordering SHALL leave revisions stable. Agent post/update/list tools SHALL advertise and decode all fields consistently, list SHALL accept an optional four-kind filter, and REST SHALL expose the same stored metadata. Only existing coordinator authorization SHALL permit agent mutations.

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


#### Scenario: Readiness preserves queued completion

- **WHEN** only trigger_pending changes after Aaron queues Done on an action or todo
- **THEN** the item becomes unread and its update is published, its authored revision remains stable, and Done plus identical receipt replay are accepted; an authored step edit still conflicts
