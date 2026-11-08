## ADDED Requirements

### Requirement: Terminal focus-out reports stay in the browser

The workspace SHALL prefetch the task's backend family when terminal input is attached, outside the serialized input path. A matched backend roster entry with kind `claude` or an explicitly empty kind `""` (the custom family on the wire) SHALL suppress an exact focus-out payload ESC [ O, matching the daemon's Claude-family session capabilities. It SHALL log the drop and issue no input request. Other resolved backend kinds, missing kind fields or missing backend matches SHALL forward focus-out unchanged. Focus-in reports and every other payload SHALL retain their bytes subject to the surgical Ctrl+Z guard. Leaving a Claude-family terminal for the composer SHALL not report blur, which suppresses Claude Code's suggested next prompt.

While classification is pending or unknown after failure, the workspace SHALL hold at most one exact focus-out report for at most two seconds, without awaiting classification on the input path. Repeated reports without intervening input SHALL NOT extend the deadline. Every other byte SHALL flow through the existing input queue immediately and in order. Any subsequently posted input SHALL mark the held report stale, and a newer focus-out MAY replace that stale report. When classification settles, the client SHALL discard held reports for Claude-family backends. For other resolved families, or an unresolved family after the two-second deadline, it SHALL forward the held report only if no input was posted after it was held, otherwise discard it as stale.

A failed prefetch SHALL log the failure and retry asynchronously after 250 ms and then one second, for at most three total attempts, adopting any successful shared API cache result. Keystrokes SHALL NOT trigger retries. Task-switch cancellation SHALL fence classification publication after the awaited read, cancel retry and hold timers, and discard held reports without canceling a shared API roster read needed by the next task.

#### Scenario: Composer focus after the terminal

- **WHEN** a resolved Claude-family agent enabled focus reporting and the terminal loses focus to the composer
- **THEN** no input request is sent for the focus-out report
- **AND** a later focus-in report is still forwarded

#### Scenario: Non-Claude focus handling

- **WHEN** a terminal for another resolved backend kind emits ESC [ O
- **THEN** the focus-out report SHALL reach the daemon unchanged
- **AND** a resolved roster with a missing backend match or kind field SHALL also preserve the report

#### Scenario: Classification is pending during input

- **WHEN** the backend roster read is pending and focus-out, ordinary bytes, Ctrl+C and composer input arrive
- **THEN** focus-out SHALL be held without delaying the other input
- **AND** the other input SHALL retain its order and make the held report stale
- **AND** once classification resolves, later focus-out reports SHALL follow the resolved family policy

#### Scenario: Cold live-stream focus reporting and composer blur

- **WHEN** a cold roster is pending while the first live output frame enables focus reporting or the interactive terminal loses focus to the composer
- **THEN** generated focus-out reports SHALL remain held in the browser
- **AND** Claude-family classification SHALL discard them
- **AND** initial snapshot replay SHALL retain its existing disabled-input fence

#### Scenario: Held report settles or reaches its deadline

- **WHEN** an exact focus-out was held and no later input was posted
- **THEN** Claude-family classification SHALL discard it and non-Claude classification SHALL forward it once
- **AND** an unresolved classification SHALL forward it once after two seconds
- **AND** stale reports or reports held by a detached input SHALL never be forwarded

#### Scenario: Transient classification failure

- **WHEN** the first prefetch fails and a later consumer successfully warms the shared backend cache
- **THEN** the next bounded asynchronous retry SHALL adopt that result for the same attached input
- **AND** ordinary input SHALL remain responsive throughout failure and retry
- **AND** repeated failures SHALL stop after three total attempts until input is reattached

#### Scenario: Task switch during shared classification

- **WHEN** a task is detached while its backend read is shared with the next task, including between promise fulfillment and continuation
- **THEN** the canceled task SHALL neither publish classification nor post late held input
- **AND** the next task SHALL continue using the shared read and its own backend match


## MODIFIED Requirements

### Requirement: Unified project task rail

The workspace SHALL always group tasks by project with coordinator nesting. Compact one-line rows SHALL retain status icons, truncated task names and live Hera role-status tags. No Project/Orchestration control or Hera-managed task filter SHALL appear. The client SHALL ignore stored rail-grouping preferences without migration. The Mac Hera sidebar mode and show-Hera-managed filter are intentional divergences because the nested project rail covers task navigation.

Search, archive, pin, actions and needs-input emphasis with collapsed-parent rollup SHALL remain available. Every task SHALL remain reachable regardless of Hera binding or project. Cross-project workers SHALL follow their visible coordinator, and filtering out a parent SHALL promote matching descendants. Selecting a coordinator SHALL open the normal task view unless split was explicitly enabled. Roster polling SHALL run only when role tags, nesting or split presentation use it, without an orchestration-grouping condition.

Role tags SHALL show blocked or failed first, then working, then ready when the role carries ready_to_close, then the role status. A working role with a live, non-idle bound task SHALL take precedence over a retained ready_to_close mark, and when a task has several live bindings the rail SHALL pick the binding by the same precedence. A row's working tag SHALL require its bound task to be live and not idle, because a self-reported working status never resets on its own. Otherwise the tag SHALL show ready when the role carries ready_to_close, idle while the session is live and stopped when it is not or the bound task is missing.

#### Scenario: Ignore obsolete rail preferences

- **WHEN** the workspace loads with a stored orchestration grouping preference
- **THEN** project groups with coordinator nesting SHALL render
- **AND** no grouping control, grouping shortcut or Hera task visibility filter SHALL appear
- **AND** split, role tags, search, archive and pin SHALL remain available

#### Scenario: Resumed worker does not show ready

- **WHEN** a live role reports working with ready_to_close set while its bound task is live and not idle
- **THEN** its role tag SHALL show working, not ready
- **AND** a done or idle role with ready_to_close SHALL still show ready

#### Scenario: Stale working status on an idle session

- **WHEN** a live role still reports working while its bound task is live and idle
- **THEN** its row tag SHALL show idle, or ready when the role carries ready_to_close
- **AND** the tag SHALL show working again once the session is running

#### Scenario: Missing bound task

- **WHEN** a live role reports working but its bound task is absent from the task snapshot
- **THEN** its tag SHALL show stopped, or ready when ready_to_close is set
- **AND** an unavailable split roster row SHALL remain disabled

#### Scenario: Failed binding remains visible among competing live bindings

- **WHEN** a task has a failed live binding alongside a working or ready live binding
- **THEN** the rail SHALL show failed regardless of binding order
- **AND** a blocked live binding SHALL take precedence over failed regardless of binding order

#### Scenario: Cross-project worker remains reachable

- **WHEN** a live Hera worker belongs to a different project than its coordinator
- **THEN** the worker SHALL appear once beneath the coordinator in the coordinator's project
- **AND** searching for the worker's project SHALL promote it into its own project when the coordinator does not match

### Requirement: Surgical terminal Ctrl+Z guard

The workspace SHALL strip SUB (0x1A) at the outbound terminal-input chokepoint, log dropped byte counts and preserve every other byte in order, subject to the Claude-family focus-out guard. A payload consisting only of SUB SHALL issue no request and SHALL not disable input. Composer validation SHALL continue to reject control-character messages before any submit.

#### Scenario: Embedded suspend byte

- **WHEN** terminal input contains ordinary bytes and Ctrl+Z
- **THEN** the daemon receives only the remaining bytes in their original order
- **AND** Ctrl+C, Ctrl+Y and escape sequences remain unchanged except the exact Claude-family focus-out payload
