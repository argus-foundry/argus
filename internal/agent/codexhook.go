package agent

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// CodexSessionHookArg is the argus subcommand a Codex SessionStart hook runs.
const CodexSessionHookArg = "codex-session-hook"

// sessionHookCommandFn resolves the hook command line. A seam so BuildCmd tests
// stay independent of the test binary's own path.
var sessionHookCommandFn = func() string {
	if isTestBinary() {
		return ""
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	return shellQuote(exe) + " " + CodexSessionHookArg
}

// codexSessionStartHookFlag registers an inline SessionStart hook that reports
// the thread ID the moment Codex mints it, so a session killed before any clean
// exit (a supervisor restart) still resumes. The trust bypass is required:
// without it Codex silently skips a hook with no recorded trusted hash, and the
// hash cannot be written without editing the user's symlinked config.toml.
func codexSessionStartHookFlag(hookCmd string) string {
	if hookCmd == "" {
		return ""
	}
	hooks := "hooks.SessionStart=[{hooks=[{type=" + strconv.Quote("command") +
		",command=" + strconv.Quote(hookCmd) + ",timeout=10}]}]"
	return " --dangerously-bypass-hook-trust -c " + shellQuote(hooks)
}

// RecordHookSessionID stores the session ID a Codex SessionStart hook reported.
// It only fills an empty ID of a Codex task and validates the format, so a
// stray or repeated report can never replace a conversation.
func RecordHookSessionID(database *db.DB, taskID, sessionID string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if !codexSessionIDRe.MatchString(sessionID) {
		return false, fmt.Errorf("unexpected session ID format: %q", sessionID)
	}
	task, err := database.Get(taskID)
	if err != nil {
		return false, err
	}
	backend, err := ResolveBackend(task, database.Config())
	if err != nil {
		return false, err
	}
	if FamilyOf(backend.Command).Kind != backendkind.Codex {
		return false, fmt.Errorf("task %s is not a Codex task", taskID)
	}
	wrote, err := database.SetSessionIDIfEmpty(taskID, sessionID)
	if err != nil {
		return false, err
	}
	if wrote {
		uxlog.Log("[codex-hook] session id recorded at launch task=%s sid=%s", taskID, sessionID)
	}
	return wrote, nil
}
