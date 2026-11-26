package mcp

import (
	"encoding/json"
	"time"

	"github.com/argus-foundry/argus/internal/voice"
)

// voiceToolDefs are the cwd-free command surface for a voice client.
// Registered only once SetVoice has wired a router. kb_ingest and kb_delete
// are not commands on this path.
var voiceToolDefs = []Tool{
	{
		Name:        "argus_brief",
		Description: "Spoken status of Argus. Returns JSON whose speech field is one to three plain sentences covering running work, idle work, work that needs input, blocked work, and work finished in the last half hour, plus a counts object. No working directory. Read speech aloud and ignore the other fields when speaking.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
	{
		Name:        "argus_commands",
		Description: "List the voice commands and the arguments each one takes. Returns JSON with a speech sentence and a commands array. Does not include kb_ingest or kb_delete.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
	{
		Name: "argus_command",
		Description: `Run one voice command by name. No working directory. Returns JSON. The speech field is the only text to read aloud: a short plain sentence, or Still running when a job has not finished.

Commands: brief, commands, start, stop, resume, send, status, wait.
start names a workflow. An existing one is resumed. A new one needs project and prompt.
stop requires confirm true.
resume accepts confirm or force to revive a closed-out workflow.
send requires task_id and text.
status and wait take job_id. wait also takes timeout_ms, capped at 3000, and returns Still running instead of blocking past that.
idempotency_key repeats the same job. A different command with the same key is rejected. kb_ingest and kb_delete are refused.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":         map[string]interface{}{"type": "string", "description": "brief, commands, start, stop, resume, send, status, or wait."},
				"idempotency_key": map[string]interface{}{"type": "string", "description": "Retry token, at most 128 characters. Same key and arguments return the same job."},
				"name":            map[string]interface{}{"type": "string", "description": "Workflow name for start, stop, or resume."},
				"project":         map[string]interface{}{"type": "string", "description": "Project. Required to create. Also disambiguates a name."},
				"prompt":          map[string]interface{}{"type": "string", "description": "Instructions for a new workflow."},
				"task_id":         map[string]interface{}{"type": "string", "description": "Task id. Required for send. Optional for start, stop, and resume."},
				"text":            map[string]interface{}{"type": "string", "description": "Text to type into the session. A return is added when missing."},
				"confirm":         map[string]interface{}{"type": "boolean", "description": "Required true for stop. Revives a closed-out workflow on resume or start."},
				"force":           map[string]interface{}{"type": "boolean", "description": "Revives a closed-out workflow on resume or start."},
				"job_id":          map[string]interface{}{"type": "string", "description": "Job handle returned by start, stop, resume, or send."},
				"timeout_ms":      map[string]interface{}{"type": "number", "description": "Wait budget in milliseconds. Capped at 3000. Zero checks once."},
			},
			"required": []string{"command"},
		},
	},
}

// SetVoice wires the voice command router. Safe to call after the listener is
// already serving. Nil clears the tools.
func (s *Server) SetVoice(r *voice.Router) {
	s.voiceMu.Lock()
	s.voice = r
	s.voiceMu.Unlock()
}

func (s *Server) voiceRouter() *voice.Router {
	s.voiceMu.RLock()
	defer s.voiceMu.RUnlock()
	return s.voice
}

func (s *Server) toolVoice(id interface{}, name string, args json.RawMessage) *Response {
	r := s.voiceRouter()
	if r == nil {
		return toolError(id, "Voice control is not available.")
	}
	switch name {
	case "argus_brief":
		return voiceToolResult(id, r.Do(s.shutdownCtx, voice.Request{Command: "brief"}))
	case "argus_commands":
		return voiceToolResult(id, r.Do(s.shutdownCtx, voice.Request{Command: "commands"}))
	case "argus_command":
		req, errText := decodeVoiceCommand(args)
		if errText != "" {
			return toolError(id, errText)
		}
		return voiceToolResult(id, r.Do(s.shutdownCtx, req))
	default:
		return toolError(id, "I don't know that command.")
	}
}

func decodeVoiceCommand(args json.RawMessage) (voice.Request, string) {
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	var raw struct {
		Command        string          `json:"command"`
		IdempotencyKey string          `json:"idempotency_key"`
		Name           string          `json:"name"`
		Project        string          `json:"project"`
		Prompt         string          `json:"prompt"`
		TaskID         string          `json:"task_id"`
		Text           string          `json:"text"`
		Confirm        bool            `json:"confirm"`
		Force          bool            `json:"force"`
		JobID          string          `json:"job_id"`
		TimeoutMS      json.RawMessage `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &raw); err != nil {
		return voice.Request{}, "Those arguments are not valid."
	}
	req := voice.Request{
		Command:        raw.Command,
		IdempotencyKey: raw.IdempotencyKey,
		Name:           raw.Name,
		Project:        raw.Project,
		Prompt:         raw.Prompt,
		TaskID:         raw.TaskID,
		Text:           raw.Text,
		Confirm:        raw.Confirm,
		Force:          raw.Force,
		JobID:          raw.JobID,
	}
	if len(raw.TimeoutMS) == 0 || string(raw.TimeoutMS) == "null" {
		return req, ""
	}
	var ms int
	if err := json.Unmarshal(raw.TimeoutMS, &ms); err != nil {
		return voice.Request{}, "timeout_ms must be a whole number of milliseconds."
	}
	if ms < 0 {
		ms = 0
	}
	req.Timeout = time.Duration(ms) * time.Millisecond
	return req, ""
}

func voiceToolResult(id interface{}, res voice.Result) *Response {
	b, err := json.Marshal(res)
	if err != nil {
		return toolError(id, "That command failed.")
	}
	if res.State == voice.StateFailed {
		return toolError(id, string(b))
	}
	return toolResult(id, string(b))
}
