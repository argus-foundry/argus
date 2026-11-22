package doctor

import (
	"github.com/argus-foundry/argus/internal/gitsign"
)

// RenderAgentSigningKey builds the advisory `argus doctor` line for the agent
// signing key (add-agent-signing-key): OFF, OK, or BROKEN with its named
// cause. Like the other advisory sections it never feeds the exit code, and
// the cause text (from gitsign) never contains key material.
func RenderAgentSigningKey(s gitsign.Status) string {
	line := "\nGit agent signing key: " + s.State
	if s.State == "BROKEN" && s.Cause != "" {
		line += ": " + s.Cause
	}
	return line + "\n"
}
