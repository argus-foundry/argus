package voice

// CommandSpec is one named voice command and the arguments it reads.
type CommandSpec struct {
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	Destructive bool      `json:"destructive"`
	Args        []ArgSpec `json:"args"`
}

// ArgSpec is one argument of a voice command.
type ArgSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// Commands is the voice allowlist. kb_ingest and kb_delete are intentionally
// absent; the router refuses those names even if a caller asks for them.
func Commands() []CommandSpec {
	key := ArgSpec{Name: "idempotency_key", Type: "string", Description: "Retry token. The same key and arguments return the same job. A different command with that key is rejected. A confirm refusal is not stored, so adding confirm can reuse the key."}
	return []CommandSpec{
		{
			Name:    "brief",
			Summary: "Say what is running, idle, waiting for input, blocked, and recently finished.",
			Args:    []ArgSpec{},
		},
		{
			Name:    "commands",
			Summary: "List the voice commands and their arguments.",
			Args:    []ArgSpec{},
		},
		{
			Name:    "start",
			Summary: "Start a workflow by name. An existing one is resumed. A new one needs a project and a prompt.",
			Args: []ArgSpec{
				{Name: "name", Type: "string", Description: "Workflow name. Use this or task id."},
				{Name: "project", Type: "string", Description: "Project. Required when creating."},
				{Name: "prompt", Type: "string", Description: "Instructions. Required when creating."},
				{Name: "task_id", Type: "string", Description: "Task id. Wins over name."},
				{Name: "confirm", Type: "boolean", Description: "Revive a closed-out workflow."},
				{Name: "force", Type: "boolean", Description: "Same as confirm for a closed-out workflow."},
				key,
			},
		},
		{
			Name:        "stop",
			Summary:     "Stop a workflow. Requires confirm.",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "task_id", Type: "string", Description: "Task id. Use this or name."},
				{Name: "name", Type: "string", Description: "Workflow name. Use this or task id."},
				{Name: "project", Type: "string", Description: "Disambiguates a repeated name."},
				{Name: "confirm", Type: "boolean", Required: true, Description: "Must be true or the stop does not run."},
				key,
			},
		},
		{
			Name:    "resume",
			Summary: "Resume a stopped workflow by task id or name. Confirm revives a closed-out one.",
			Args: []ArgSpec{
				{Name: "task_id", Type: "string", Description: "Task id. Use this or name."},
				{Name: "name", Type: "string", Description: "Workflow name. Use this or task id."},
				{Name: "project", Type: "string", Description: "Disambiguates a repeated name."},
				{Name: "confirm", Type: "boolean", Description: "Revive a closed-out workflow."},
				{Name: "force", Type: "boolean", Description: "Same as confirm for a closed-out workflow."},
				key,
			},
		},
		{
			Name:    "send",
			Summary: "Send text to a live session. Requires a task id.",
			Args: []ArgSpec{
				{Name: "task_id", Type: "string", Required: true, Description: "Task id of the live session."},
				{Name: "text", Type: "string", Required: true, Description: "What to type. A return is added when missing."},
				key,
			},
		},
		{
			Name:    "status",
			Summary: "Read a job. Says still running when the job has not finished. Does not wait.",
			Args: []ArgSpec{
				{Name: "job_id", Type: "string", Required: true, Description: "Job handle from start, stop, resume, or send."},
			},
		},
		{
			Name:    "wait",
			Summary: "Wait briefly for a job. Times out with still running instead of blocking.",
			Args: []ArgSpec{
				{Name: "job_id", Type: "string", Required: true, Description: "Job handle."},
				{Name: "timeout_ms", Type: "integer", Description: "How long to wait, capped at 3000 milliseconds. Zero checks once."},
			},
		},
	}
}
