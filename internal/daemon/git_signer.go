package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/gitsign"
)

// errGitSigningOff is the named cause POST /api/git/sign answers (HTTP 503)
// while the live policy is not agent-key. It matches the route's own
// unset-signer message.
var errGitSigningOff = errors.New("agent commit signing not configured (git.agent_commit_signing is not agent-key)")

// newGitSigner returns the daemon's POST /api/git/sign signer
// (add-agent-signing-key D5).
//
// Config live-reload: the policy and key reference are read from cfgFn() on
// EVERY request instead of once at startup, so switching
// git.agent_commit_signing in config.toml (mtime live-reload) takes effect on
// the next commit without a daemon restart, in both directions. The signer is
// therefore always wired; while the policy is off it answers the same named
// 503 the route gives for an unset signer.
//
// One gitsign.KeyHolder is memoized per key reference, so the private key is
// resolved once per reference for the life of the process (success-only, per
// KeyHolder). A changed reference gets a fresh holder; holders for old
// references stay cached (a handful at most) so switching back is free. The
// holder's resolver reads [secrets] live too, so fixing the 1Password
// bootstrap after a failed resolve needs no restart either.
func newGitSigner(cfgFn func() config.Config, resolve func(config.SecretsConfig, string) (string, bool)) api.GitSignFunc {
	var (
		mu      sync.Mutex
		holders = map[string]*gitsign.KeyHolder{}
	)
	liveResolve := func(source string) (string, bool) {
		return resolve(cfgFn().Secrets, source)
	}
	holderFor := func(ref string) *gitsign.KeyHolder {
		mu.Lock()
		defer mu.Unlock()
		h, ok := holders[ref]
		if !ok {
			h = gitsign.NewKeyHolder(liveResolve, ref)
			holders[ref] = h
		}
		return h
	}
	return func(_ context.Context, _, namespace string, data []byte) ([]byte, error) {
		cfg := cfgFn()
		if cfg.Git.AgentCommitSigning != config.GitSigningAgentKey {
			return nil, errGitSigningOff
		}
		s, err := holderFor(strings.TrimSpace(cfg.Git.AgentSigningKey)).Signer()
		if err != nil {
			return nil, err
		}
		return gitsign.Sign(s, namespace, data)
	}
}
