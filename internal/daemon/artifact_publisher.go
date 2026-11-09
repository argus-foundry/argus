package daemon

import (
	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/buildid"
	"github.com/argus-foundry/argus/internal/claudeartifacts"
	"github.com/argus-foundry/argus/internal/config"
)

// newArtifactPublisher builds the daemon's claude.ai artifact publisher, the
// one instance the MCP artifact_publish tool and POST /api/artifacts/publish
// share. Tokens come from [artifact_tokens] through agent.Resolve, inside the
// daemon process at publish time (add-argus-artifact-publish D1/D2): they are
// never exported to an agent. Resolve memoizes only successes, so a token
// rotated in its secret store needs a daemon restart to be picked up.
func newArtifactPublisher(cfgFn func() config.Config) *claudeartifacts.Publisher {
	return &claudeartifacts.Publisher{
		Client:  &claudeartifacts.Client{UserAgent: artifactUserAgent(buildid.Current())},
		Config:  cfgFn,
		Resolve: agent.Resolve,
	}
}

// artifactUserAgent names this build in requests to the artifacts endpoint.
func artifactUserAgent(v buildid.VCS) string {
	if !v.Present() {
		return "argus"
	}
	rev := v.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return "argus/" + rev
}
