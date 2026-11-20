package mcp

import (
	"encoding/json"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type boardStore interface {
	PostHeraBoardItem(int64, string, string, string, ...*db.HeraBoardStructure) (*db.HeraBoardItem, error)
	UpdateHeraBoardItem(int64, int64, string, string, string, ...*db.HeraBoardStructure) (*db.HeraBoardItem, error)
	ResolveHeraBoardItem(int64, int64, string, string) error
	ListHeraBoardItems(int64, bool) ([]*db.HeraBoardItem, error)
}

// boardBodyGuidance tells coordinators how to author a board body; the TUI
// renders newlines, bullets, nested bullets, numbered items and **bold**.
const boardBodyGuidance = "Format every body for scanning: open with one short sentence naming the ask or outcome, then a blank line, then one fact or option per `- **Label:** detail` bullet (indent two spaces for `  - ` sub-bullets; `1. ` for ordered steps). For decisions with distinct options, use 2-4 structured options with id, label, summary, upside and downside. Always supply recommended and reason; body states only the situation. Tell Aaron to press o on the board to answer the round. One Aaron (board) summary message records the already-resolved answers; act on them and follow up on Still open ids. Something else requires a note, which is the answer. Omitted update options preserves structure; options: [] clears it. Put each link or PR URL on its own bullet. Never write a wall of prose."

const boardAnnotationGuidance = " Feedback includes COMMENT and DELETION annotations with quoted text and comments: address every annotation. DELETION is a suggestion, never an applied board edit; comment-only decisions remain open. Retrieve full shortened feedback at authenticated GET /api/hera/roles/{role_id}/board/fyi-actions/{action_id} through the API proxy (master/device only, never a page session). Keep settled follow-up reflected on the board."

func boardToolDefs() []Tool {
	var out []Tool
	for _, verb := range []string{"post", "update", "resolve", "list"} {
		required := []string{"cwd"}
		switch verb {
		case "post":
			required = append(required, "kind", "subject", "body")
		case "update":
			required = append(required, "id", "kind", "subject", "body")
		case "resolve":
			required = append(required, "id")
		}
		out = append(out, Tool{Name: "hera_board_" + verb, Description: "Coordinator-only human attention board: " + verb + " items owned by your coordinator role. Put things needing Aaron's decision or attention on your board instead of only in chat. Rewrite an item when newer information supersedes it; resolve when settled, including after Aaron replies and follow-up concludes. Material edits mark unread. update replaces kind, subject and body; list defaults to open items. Body format: " + boardBodyGuidance + boardAnnotationGuidance, InputSchema: map[string]interface{}{
			"type": "object", "required": required, "properties": map[string]interface{}{
				"cwd": map[string]string{"type": "string"}, "orchestrator": map[string]string{"type": "string"}, "id": map[string]string{"type": "integer"},
				"options":     map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"id", "label"}, "properties": map[string]any{"id": map[string]string{"type": "string"}, "label": map[string]string{"type": "string"}, "summary": map[string]string{"type": "string"}, "upside": map[string]string{"type": "string"}, "downside": map[string]string{"type": "string"}}}},
				"recommended": map[string]string{"type": "string"}, "reason": map[string]string{"type": "string"},
				"kind": map[string]interface{}{"type": "string", "enum": []string{"decision", "fyi"}}, "subject": map[string]string{"type": "string"}, "body": map[string]string{"type": "string"}, "note": map[string]string{"type": "string"}, "include_resolved": map[string]string{"type": "boolean"},
			},
		}})
	}
	return out
}

func (s *Server) toolHeraBoard(id interface{}, args json.RawMessage, verb string) *Response {
	if !s.heraEnabled() {
		return toolError(id, "hera not configured")
	}
	var p boardToolParams
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(id, "invalid board arguments")
	}
	if p.Cwd == "" {
		return toolError(id, "cwd is required")
	}
	caller, err := s.resolveCallerRole(p.Cwd, p.Orchestrator)
	if err != nil {
		return toolError(id, err.Error())
	}
	if caller.role.Kind != db.HeraKindCoordinator {
		// The builtin hera-remind-me skill matches this exact text to detect a
		// worker/freelance caller and relay via hera_send; reword both together.
		return toolError(id, "board tools require a coordinator binding; report to your coordinator")
	}
	store, ok := s.heraStore.(boardStore)
	if !ok {
		return toolError(id, "board store unavailable")
	}
	var structure []*db.HeraBoardStructure
	if p.Options != nil {
		var v *db.HeraBoardStructure
		if len(*p.Options) > 0 {
			v = &db.HeraBoardStructure{Options: *p.Options}
		}
		if v != nil {
			if p.Recommended != nil {
				v.Recommended = *p.Recommended
			}
			if p.Reason != nil {
				v.Reason = *p.Reason
			}
		}
		structure = append(structure, v)
	} else if p.Recommended != nil || p.Reason != nil {
		items, e := store.ListHeraBoardItems(caller.role.ID, false)
		if e != nil {
			return toolError(id, "board unavailable")
		}
		var v *db.HeraBoardStructure
		for _, i := range items {
			if i.ID == p.ID && i.Structured != nil {
				copy := *i.Structured
				v = &copy
			}
		}
		if v == nil {
			return toolError(id, "recommendation requires options")
		}
		if p.Recommended != nil {
			v.Recommended = *p.Recommended
		}
		if p.Reason != nil {
			v.Reason = *p.Reason
		}
		structure = append(structure, v)
	}
	var result any
	switch verb {
	case "post":
		result, err = store.PostHeraBoardItem(caller.role.ID, p.Kind, p.Subject, p.Body, structure...)
	case "update":
		result, err = store.UpdateHeraBoardItem(caller.role.ID, p.ID, p.Kind, p.Subject, p.Body, structure...)
	case "resolve":
		err = store.ResolveHeraBoardItem(caller.role.ID, p.ID, "agent", p.Note)
		result = "resolved"
	case "list":
		result, err = store.ListHeraBoardItems(caller.role.ID, p.IncludeResolved)
	}
	if err != nil {
		uxlog.Log("[board] MCP %s failed role=%d: %v", verb, caller.role.ID, err)
		return toolError(id, err.Error())
	}
	uxlog.Log("[board] MCP %s succeeded role=%d", verb, caller.role.ID)
	raw, err := json.Marshal(result)
	if err != nil {
		return toolError(id, err.Error())
	}
	return toolResult(id, string(raw))
}

type boardToolParams struct {
	Cwd             string                `json:"cwd"`
	Orchestrator    string                `json:"orchestrator"`
	ID              int64                 `json:"id"`
	Kind            string                `json:"kind"`
	Subject         string                `json:"subject"`
	Body            string                `json:"body"`
	Note            string                `json:"note"`
	IncludeResolved bool                  `json:"include_resolved"`
	Options         *[]db.HeraBoardOption `json:"options"`
	Recommended     *string               `json:"recommended"`
	Reason          *string               `json:"reason"`
}
