package agent

import (
	"errors"
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

// ErrInvalidSessionReport marks a hook report that is malformed or targets a non-Codex task.
var ErrInvalidSessionReport = errors.New("invalid session report")

// sessionHookCommandFn is a seam so BuildCmd tests don't depend on the test binary's path.
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

// codexSessionStartHookFlag needs the trust bypass: Codex skips a hook with no trusted hash, and config.toml is a symlink we must not edit.
func codexSessionStartHookFlag(hookCmd string) string {
	if hookCmd == "" {
		return ""
	}
	hooks := "hooks.SessionStart=[{hooks=[{type=" + strconv.Quote("command") +
		",command=" + strconv.Quote(hookCmd) + ",timeout=10}]}]"
	return " --dangerously-bypass-hook-trust -c " + shellQuote(hooks)
}

// RecordHookSessionID stores a hook-reported Codex ID: fill-only, or a replace for a "clear" start (/new).
func RecordHookSessionID(database *db.DB, taskID, sessionID string, replace bool) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if !codexSessionIDRe.MatchString(sessionID) {
		return false, fmt.Errorf("%w: unexpected session ID format %q", ErrInvalidSessionReport, sessionID)
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
		return false, fmt.Errorf("%w: task %s is not a Codex task", ErrInvalidSessionReport, taskID)
	}
	var wrote bool
	if replace {
		wrote, err = database.ReplaceSessionID(taskID, sessionID)
	} else {
		wrote, err = database.SetSessionIDIfEmpty(taskID, sessionID)
	}
	if err != nil {
		return false, err
	}
	if !wrote && !replace {
		uxlog.Log("[codex-hook] session id already recorded task=%s; startup report ignored", taskID)
	}
	if wrote {
		uxlog.Log("[codex-hook] session id recorded task=%s sid=%s replace=%t", taskID, sessionID, replace)
	}
	return wrote, nil
}
