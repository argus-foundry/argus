package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// `argus codex-session-hook` is the Codex SessionStart hook Argus injects at
// launch (agent.codexSessionStartHookFlag). It reports the new thread's
// session_id to the daemon so the ID is stored before any exit; a supervisor
// restart kills Codex without the post-exit capture ever running. It never
// writes to stdout (stdout is parsed as hook output) and always exits 0 so a
// daemon problem can never block the agent from starting.
func runCodexSessionHookCommand() {
	_ = runCodexSessionHook(os.Stdin, os.Getenv("ARGUS_TASK_ID"), reportSessionIDReal)
}

// runCodexSessionHook decodes the hook payload and reports its session_id.
// report is the daemon seam. Returns the error it swallowed, for tests.
func runCodexSessionHook(stdin io.Reader, taskID string, report func(taskID, sessionID, source string) error) error {
	if taskID == "" {
		return nil // not an Argus-spawned session
	}
	var payload struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&payload); err != nil {
		return fmt.Errorf("decode hook payload: %w", err)
	}
	// "startup" names the task's conversation; "clear" is a /new inside it, which
	// moves the conversation to the new thread (as Claude's /clear is honored).
	// Any other source (resume, compact) must never be recorded.
	source := payload.Source
	if source == "" {
		source = "startup"
	}
	if source != "startup" && source != "clear" {
		return nil
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		return fmt.Errorf("hook payload has no session_id")
	}
	if err := report(taskID, payload.SessionID, source); err != nil {
		uxlog.Log("[codex-hook] report failed task=%s: %v", taskID, err)
		return err
	}
	return nil
}

func reportSessionIDReal(taskID, sessionID, source string) error {
	body, err := json.Marshal(map[string]string{"session_id": sessionID, "source": source})
	if err != nil {
		return err
	}
	_, err = coordHookRequest(http.MethodPost, "/api/tasks/"+taskID+"/session-id", bytes.NewReader(body))
	return err
}
