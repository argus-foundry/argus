package claudeartifacts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/config"
)

// ErrNoToken: the account has no usable [artifact_tokens] entry, or its
// secret did not resolve. No request was made.
var ErrNoToken = errors.New("no artifact token for account")

// defaultProbeTimeout bounds one account probe on the unavailable path.
const defaultProbeTimeout = 15 * time.Second

// Publisher publishes as a named account: it looks the account up in
// [artifact_tokens], resolves the secret inside this (the daemon's) process
// at publish time, and calls the endpoint. Tokens never leave it.
type Publisher struct {
	Client *Client
	// Config returns the live config; read on every call so an
	// [artifact_tokens] edit needs no restart.
	Config func() config.Config
	// Resolve resolves a secret source (agent.Resolve in the daemon).
	Resolve func(sc config.SecretsConfig, source string) (string, bool)
	// ProbeTimeout bounds each AvailableAccounts probe; zero means 15s.
	ProbeTimeout time.Duration
}

func (p *Publisher) probeTimeout() time.Duration {
	if p.ProbeTimeout <= 0 {
		return defaultProbeTimeout
	}
	return p.ProbeTimeout
}

// token resolves account's token from cfg.
func (p *Publisher) token(cfg config.Config, account string) (string, error) {
	src, err := cfg.ArtifactTokenSource(account)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoToken, err)
	}
	tok, ok := p.Resolve(cfg.Secrets, src)
	if !ok || tok == "" {
		return "", fmt.Errorf("%w: could not resolve the [artifact_tokens] secret for account %q", ErrNoToken, account)
	}
	return tok, nil
}

// Publish publishes in as account. Errors wrap ErrInvalidInput, ErrNoToken,
// ErrUnauthorized or ErrAccountUnavailable where they apply.
func (p *Publisher) Publish(ctx context.Context, account string, in PublishInput) (Result, error) {
	if err := ValidateHTML(in.Name, in.Content); err != nil {
		return Result{}, err
	}
	if in.UpdateURL != "" {
		if _, err := ParseArtifactSlug(in.UpdateURL); err != nil {
			return Result{}, err
		}
	}
	tok, err := p.token(p.Config(), account)
	if err != nil {
		return Result{}, err
	}
	res, err := p.Client.Publish(ctx, tok, in)
	switch {
	case errors.Is(err, ErrUnauthorized):
		return Result{}, fmt.Errorf("%w: the token for account %q was rejected (expired or revoked). Re-run `claude setup-token` signed in as that account, store the new token in the secret its [artifact_tokens] entry names, then restart the Argus daemon (resolved secrets are cached for its lifetime)", err, account)
	case errors.Is(err, ErrAccountUnavailable):
		return Result{}, fmt.Errorf("account %q: %w", account, err)
	case err != nil:
		return Result{}, fmt.Errorf("account %q: %w", account, err)
	}
	return res, nil
}

// AvailableAccounts probes every [artifact_tokens] account except exclude,
// concurrently, and returns (sorted) those that answered a real tool call.
// An account whose entry is invalid, whose secret does not resolve, or whose
// probe fails or times out is left out.
func (p *Publisher) AvailableAccounts(ctx context.Context, exclude string) []string {
	cfg := p.Config()
	var (
		mu  sync.Mutex
		out []string
		wg  sync.WaitGroup
	)
	for _, account := range cfg.ArtifactTokenAccounts() {
		if account == exclude {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := p.token(cfg, account)
			if err != nil {
				return
			}
			pctx, cancel := context.WithTimeout(ctx, p.probeTimeout())
			defer cancel()
			if p.Client.Probe(pctx, tok) != nil {
				return
			}
			mu.Lock()
			out = append(out, account)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(out)
	return out
}
