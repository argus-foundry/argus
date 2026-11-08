package agent

import (
	"time"

	"github.com/drn/argus/internal/codexctx"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/skills"
)

const (
	ContextProviderOpenAI = "openai"
	ContextSourceCodex    = "codex_rollout"
)

// ContextReading is one backend-neutral context measurement. WindowTokens is 0
// when the window is unknown.
type ContextReading struct {
	Provider         string
	Source           string
	SessionID        string
	MeasuredAt       time.Time
	SessionStartedAt time.Time
	UsedTokens       int64
	WindowTokens     int64
}

// readCodexContext measures the task's recorded Codex conversation, resolved
// through its own account's home and scoped to its worktree and session.
func readCodexContext(task *model.Task, cfg config.Config) (ContextReading, bool, error) {
	home, err := CodexHomeForTask(task, cfg)
	if err != nil {
		return ContextReading{}, false, err
	}
	stateDB, err := CodexStateDBPath(home)
	if err != nil {
		return ContextReading{}, false, err
	}
	th, ok, err := codexctx.FindThread(stateDB, task.Worktree, task.SessionID)
	if err != nil || !ok {
		return ContextReading{}, false, err
	}
	if home == "" {
		home, err = skills.UserCodexHome()
		if err != nil {
			return ContextReading{}, false, err
		}
	}
	overlay, err := skills.ArgusCodexHomeFor(home)
	if err != nil {
		return ContextReading{}, false, err
	}
	r, started, ok, err := codexctx.LatestForThread(home, task.Worktree, th, overlay)
	if err != nil || !ok {
		return ContextReading{}, false, err
	}
	return ContextReading{
		Provider: ContextProviderOpenAI, Source: ContextSourceCodex, SessionID: th.ID,
		SessionStartedAt: started, MeasuredAt: r.MeasuredAt, UsedTokens: r.UsedTokens, WindowTokens: r.WindowTokens,
	}, true, nil
}
