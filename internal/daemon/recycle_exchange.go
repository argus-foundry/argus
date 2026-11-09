package daemon

import (
	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// recycleExchangeSection returns the "Last exchange with the user" seed-prompt
// section extracted from task's PRIOR Claude transcript, or "" when there is
// nothing to relay. It MUST be called before task.SessionID is cleared and
// never fails or delays the recycle (see agent.ClaudeLastExchange).
func recycleExchangeSection(task *model.Task, cfg config.Config) string {
	return agent.ClaudeLastExchange(task, cfg).Section()
}
