package model

import (
	"encoding/json"
	"time"
)

// Event is one row in the daemon-wide events ring (PR 2 of the plugin
// substrate). Plugins consume events through the /api/events/stream SSE
// channel; the ring also backs replay on reconnect via the `since` cursor.
//
// Payload travels as raw JSON so each event type can evolve its body without
// requiring schema migrations on the events table. The shape per type is
// documented alongside the EventType* constants below.
type Event struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	At      time.Time       `json:"at"`
	TaskID  string          `json:"task_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Canonical event type strings. Plugins rely on these being stable — changing
// any value here is a breaking change to the plugin contract (would require
// the X-Argus-Plugin-Version major bump described in docs/plugins.md).
const (
	EventTypeTaskDeleteJobUpdated = "task_delete_job.updated"
	EventTypeTaskCreated          = "task.created"
	EventTypeTaskStatusChanged    = "task.status_changed"
	EventTypeTaskCompleted        = "task.completed"
	EventTypeTaskArchived         = "task.archived"
	EventTypeTaskDeleted          = "task.deleted"
	EventTypeTaskRenamed          = "task.renamed"
	EventTypeTaskUpdated          = "task.updated"
	// EventTypeTaskFailureChanged carries {"failure": null | TaskFailure}
	// after a Result write changes the explicit-failure projection.
	EventTypeTaskFailureChanged = "task.failure_changed"
	EventTypeTaskForked         = "task.forked"
	// EventTypeTaskPRChanged carries PRChangedPayload {"url","state","lifecycle","source"},
	// published after a changed PR cache commit by the daemon poller or a
	// manual PR link change.
	EventTypeTaskPRChanged = "task.pr_changed"
	// EventTypeTaskRuntimeChanged fires after a PATCH /api/tasks/{id}/runtime
	// settles. Payload: {"apply":"next_resume"|"restart","changed_fields":[...],
	// "started":bool}. Never carries prompt text.
	EventTypeTaskRuntimeChanged = "task.runtime_changed"

	// EventTypeTaskAccountChanged fires after POST /api/tasks/{id}/account
	// persisted a new account. Payload: {"from","to","resumed","started"};
	// started:false after a persisted-but-failed restart lets callers reconcile.
	EventTypeTaskAccountChanged = "task.account_changed"
	EventTypeMessageSent        = "message.sent"
	EventTypeMessageAcked       = "message.acked"
	EventTypeSessionStarted     = "session.started"
	EventTypeSessionExited      = "session.exited"
	EventTypeSessionIdle        = "session.idle"

	// EventTypeSessionNeedsInput fires when a session transitions into or out
	// of the "blocked waiting on the user" state (the red ? in the TUI). One
	// event type carries both edges; the payload's needs_input bool
	// distinguishes enter (true) from clear (false), e.g.
	// {"needs_input":true}. Emitted by the daemon's idle/needs-input watcher
	// (internal/api/push.go) so external consumers render the signal without
	// re-deriving argus's detection heuristic from session logs.
	EventTypeSessionNeedsInput = "session.needs_input"

	// EventTypeSessionRuntimeChanged carries {"live","idle","needs_input"}
	// whenever any of the task's REST runtime flags changes.
	EventTypeSessionRuntimeChanged = "session.runtime_changed"

	// EventTypeSessionFocus fires when the TUI's agent view gains or loses
	// human focus for a task. Payload: {"focused":true|false}. Emitted by
	// the FocusTracker transition callback so plugins can observe human
	// presence without polling. Persisted to the events table like all other
	// event types; consumed by SSE subscribers.
	EventTypeSessionFocus = "session.focus"

	// EventTypeTaskContextChanged invites a GET /api/tasks/{id}/context after a
	// fresh context measurement. Payload: {"session_id":"s","measured_at":"RFC3339"};
	// identity and timing only, never the counts themselves.
	EventTypeTaskContextChanged = "task.context_changed"

	// EventTypeTaskSuggestionChanged invites a GET /api/tasks/{id}/bridge when
	// the provider's suggested next prompt appears or is cleared. Payload:
	// {"session_id":"s","available":true,"recorded_at":"RFC3339"}; never the text.
	EventTypeTaskSuggestionChanged = "task.suggestion_changed"

	// EventTypeHeraBoardChanged fires after a committed human-board post,
	// material edit, read-state change or resolution. Payload:
	// {"role_id":2,"item_id":3,"unread_count":1,"operation":"posted|updated|read_state|resolved"}.
	// Never carries item bodies; TaskID is the coordinator's bound task when known.
	EventTypeHeraBoardChanged = "hera.board_changed"

	// EventTypeResidentPrefix prefixes every event about the resident agent
	// (add-resident-agent). The events bus rewrites any event whose task id is
	// the resident's to "resident.<original type>" (e.g. resident.session.idle,
	// resident.task.updated) so the resident never appears as a task.* or
	// session.* event in the task SSE stream.
	EventTypeResidentPrefix = "resident."

	// EventTypeResidentCreated fires once when the daemon creates the
	// resident row. Payload: {"name":"resident"}.
	EventTypeResidentCreated = EventTypeResidentPrefix + "created"

	// EventTypeTaskUsageChanged invites a GET /api/tasks/{id}/usage after a
	// changed token observation. Payload: {"session_id":"s","turn_id":"t","measured_at":"RFC3339"};
	// identity and timing only, never the counts themselves.
	EventTypeTaskUsageChanged = "task.usage_changed"

	// EventTypeConfigChanged fires after a committed settings write so clients
	// refetch the named resource. Payload carries identity only, e.g.
	// {"resource":"accounts","name":"work"} or
	// {"resource":"account_defaults","project":"argus"} (project omitted for
	// the global default).
	EventTypeConfigChanged = "config.changed"

	// EventTypeAccountUsageChanged fires when an account's projected provider
	// usage changes. Payload is identity and timing only, no usage values:
	// {"account":"work","kind":"codex","measured_at":"..."} (kind is "claude"
	// or "codex"; measured_at omitted when usage became unavailable).
	EventTypeAccountUsageChanged = "account.usage_changed"

	// EventTypeResync is synthesised by the SSE handler when a client
	// connects with a `since` cursor older than the oldest retained event.
	// It is never persisted in the events table — clients seeing it should
	// resnapshot the daemon's current state.
	EventTypeResync = "resync"
)

// Field names carried in a task.updated payload's changed_fields array. The
// payload is identity plus this aggregate only — never prompt, result, or
// credential content — so clients treat the event as an invalidation and
// re-read the task over REST.
const (
	TaskFieldName     = "name"
	TaskFieldStatus   = "status"
	TaskFieldArchived = "archived"
	TaskFieldPinned   = "pinned"
	TaskFieldRuntime  = "runtime"
	TaskFieldAccount  = "account"
	TaskFieldResult   = "result"
	TaskFieldPR       = "pr"
)

// TaskUpdatedFields is the canonical, ordered changed_fields vocabulary.
var TaskUpdatedFields = []string{
	TaskFieldName, TaskFieldStatus, TaskFieldArchived, TaskFieldPinned,
	TaskFieldRuntime, TaskFieldAccount, TaskFieldResult, TaskFieldPR,
}

// ChangedTaskFields reports which task.updated fields differ between two task
// snapshots. runtime covers backend, model, sandbox override, and session identity.
func ChangedTaskFields(old, cur *Task) []string {
	if old == nil || cur == nil {
		return nil
	}
	var out []string
	add := func(changed bool, f string) {
		if changed {
			out = append(out, f)
		}
	}
	add(old.Name != cur.Name, TaskFieldName)
	add(old.Status != cur.Status, TaskFieldStatus)
	add(old.Archived != cur.Archived, TaskFieldArchived)
	add(old.Pinned != cur.Pinned, TaskFieldPinned)
	add(old.Backend != cur.Backend || old.Model != cur.Model || old.SessionID != cur.SessionID ||
		old.SandboxOverride != cur.SandboxOverride, TaskFieldRuntime)
	add(old.Account != cur.Account || old.AccountRouted != cur.AccountRouted, TaskFieldAccount)
	add(old.Result != cur.Result, TaskFieldResult)
	return out
}
