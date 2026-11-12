package agent

import (
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// HasClaudeTranscript reports whether task runs on the Claude family (the only
// one that keeps an on-disk JSONL transcript Argus can inspect). Bounce
// recovery uses it to tell "no transcript to classify (non-Claude)" from
// "Claude task whose transcript is gone", which must never be resumed: a fresh
// launch re-runs the original prompt.
func HasClaudeTranscript(task *model.Task, cfg config.Config) bool {
	backend, err := ResolveBackend(task, cfg)
	if err != nil {
		return false
	}
	return FamilyOf(backend.Command).ResumeAvailable != nil
}
