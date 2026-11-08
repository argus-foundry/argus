package events

import "github.com/drn/argus/internal/model"

// EmitTaskUpdated publishes the shared task.updated invalidation event. Every
// mutation path (REST, MCP, TUI, daemon watchers) reaches it through the
// internal/db setters, so a semantic change emits it exactly once. Call only
// after the write committed and with no DB or runner lock held. Fields are
// deduplicated into canonical order and unknown names are dropped; with
// nothing left the call is a no-op, which keeps no-op writes silent.
func EmitTaskUpdated(taskID string, fields ...string) {
	if taskID == "" || len(fields) == 0 {
		return
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		seen[f] = true
	}
	changed := make([]string, 0, len(seen))
	for _, f := range model.TaskUpdatedFields {
		if seen[f] {
			changed = append(changed, f)
		}
	}
	if len(changed) == 0 {
		return
	}
	Emit(model.EventTypeTaskUpdated, taskID, map[string][]string{"changed_fields": changed})
}
