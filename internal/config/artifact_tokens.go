package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNoArtifactToken reports that an account has no [artifact_tokens] entry.
var ErrNoArtifactToken = errors.New("no artifact token configured")

// ErrArtifactTokenNotReference reports an [artifact_tokens] value that is not
// a secret reference (most likely a token pasted in the clear).
var ErrArtifactTokenNotReference = errors.New("artifact token is not a secret reference")

// artifactTokenSchemes are the secret-source schemes an [artifact_tokens]
// value may use. A bare string (the resolver's implicit env:// form) is NOT
// accepted here: that is exactly what a pasted literal token looks like.
var artifactTokenSchemes = []string{"op://", "keychain://", "env://"}

// IsSecretReference reports whether v is an explicit secret reference with a
// non-empty target: op://…, keychain://… or env://….
func IsSecretReference(v string) bool {
	for _, scheme := range artifactTokenSchemes {
		if rest, ok := strings.CutPrefix(v, scheme); ok && strings.TrimSpace(rest) != "" {
			return true
		}
	}
	return false
}

// ArtifactTokenSource returns the secret source configured for account in
// [artifact_tokens]. It fails with ErrNoArtifactToken when there is no entry
// and ErrArtifactTokenNotReference when the entry is not a secret reference.
// Errors name the account and the table but never echo the configured value,
// which could be a pasted token.
func (c Config) ArtifactTokenSource(account string) (string, error) {
	src, ok := c.ArtifactTokens[account]
	if !ok {
		return "", fmt.Errorf("%w: no [artifact_tokens] entry for account %q", ErrNoArtifactToken, account)
	}
	src = strings.TrimSpace(src)
	if !IsSecretReference(src) {
		return "", fmt.Errorf("%w: [artifact_tokens] entry for account %q is not a secret reference (use op://, keychain:// or env://)", ErrArtifactTokenNotReference, account)
	}
	return src, nil
}

// ArtifactTokenAccounts lists the accounts with an [artifact_tokens] entry,
// sorted.
func (c Config) ArtifactTokenAccounts() []string {
	out := make([]string, 0, len(c.ArtifactTokens))
	for name := range c.ArtifactTokens {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
