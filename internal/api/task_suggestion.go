package api

import (
	"time"

	"github.com/drn/argus/internal/bridge"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
)

func (s *Server) bridgeSuggestion(id string) *bridge.Suggestion {
	st, ok := s.bridgeHub.Get(id)
	if !ok {
		return nil
	}
	return st.Suggestion
}

// emitSuggestionChange emits task.suggestion_changed when the stored
// suggestion appeared, was replaced or was cleared. The payload carries
// identity and availability only; consumers read the text with GET.
func (s *Server) emitSuggestionChange(id string, before, after *bridge.Suggestion) {
	if before == nil && after == nil {
		return
	}
	if before != nil && after != nil && before.RecordedAt.Equal(after.RecordedAt) && before.Text == after.Text {
		return
	}
	payload := map[string]any{"available": after != nil}
	switch {
	case after != nil:
		payload["session_id"] = after.SessionID
		payload["recorded_at"] = after.RecordedAt.UTC().Format(time.RFC3339Nano)
	default:
		payload["session_id"] = before.SessionID
		payload["recorded_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	uxlog.Log("[bridge] suggestion task=%s available=%t", id, after != nil)
	events.Emit(model.EventTypeTaskSuggestionChanged, id, payload)
}
