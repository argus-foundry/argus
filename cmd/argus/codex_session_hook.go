package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// runCodexSessionHookCommand is the Codex SessionStart hook; it must print nothing to stdout and always exit 0.
func runCodexSessionHookCommand() {
	if err := runCodexSessionHook(os.Stdin, os.Getenv("ARGUS_TASK_ID"), reportSessionIDReal); err != nil {
		fmt.Fprintln(os.Stderr, "argus codex-session-hook:", err) // uxlog is not initialised in this process
	}
}

// runCodexSessionHook reports the payload's session_id through the report seam and returns the error it swallowed.
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
	source := payload.Source
	if source == "" {
		source = "startup"
	}
	// "clear" is a /new inside the session; resume and compact starts are never recorded.
	if source != "startup" && source != "clear" {
		return nil
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		return fmt.Errorf("hook payload has no session_id")
	}
	return report(taskID, payload.SessionID, source)
}

func reportSessionIDReal(taskID, sessionID, source string) error {
	body, err := json.Marshal(map[string]string{"session_id": sessionID, "source": source})
	if err != nil {
		return err
	}
	_, err = coordHookRequest(http.MethodPost, "/api/tasks/"+taskID+"/session-id", bytes.NewReader(body))
	return err
}
