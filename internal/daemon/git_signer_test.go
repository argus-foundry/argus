package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/testutil"
)

const gsRef = "op://claude/Argus agent signing key"

func gsKey(t *testing.T) (pub, priv string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sp, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sk, "")
	testutil.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))), string(pem.EncodeToMemory(block))
}

// gsEnv is a mutable live config plus a counting resolver.
type gsEnv struct {
	mu     sync.Mutex
	cfg    config.Config
	values map[string]string
	calls  map[string]int
	sc     []config.SecretsConfig
}

func (e *gsEnv) cfgFn() config.Config { e.mu.Lock(); defer e.mu.Unlock(); return e.cfg }

func (e *gsEnv) resolve(sc config.SecretsConfig, src string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls[src]++
	e.sc = append(e.sc, sc)
	v, ok := e.values[src]
	return v, ok
}

func newGSEnv(policy, ref string, values map[string]string) *gsEnv {
	return &gsEnv{
		cfg:    config.Config{Git: config.GitConfig{AgentCommitSigning: policy, AgentSigningKey: ref}},
		values: values,
		calls:  map[string]int{},
	}
}

func TestNewGitSigner(t *testing.T) {
	pub, priv := gsKey(t)
	good := map[string]string{gitsign.PublicKeyRef(gsRef): pub, gitsign.PrivateKeyRef(gsRef): priv}

	t.Run("policy off answers named error without resolving", func(t *testing.T) {
		for _, policy := range []string{"", "inherit", "unsigned"} {
			e := newGSEnv(policy, gsRef, good)
			sig, err := newGitSigner(e.cfgFn, e.resolve)(context.Background(), "t1", "git", []byte("x"))
			testutil.ErrorIs(t, err, errGitSigningOff)
			testutil.Equal(t, len(sig), 0)
			testutil.Equal(t, len(e.calls), 0)
		}
	})

	t.Run("agent-key signs and memoizes the key", func(t *testing.T) {
		e := newGSEnv("agent-key", "  "+gsRef+" ", good)
		e.cfg.Secrets = config.SecretsConfig{Op: config.OpConfig{BootstrapSource: "keychain://x"}}
		f := newGitSigner(e.cfgFn, e.resolve)
		for range 2 {
			sig, err := f(context.Background(), "t1", "git", []byte("data"))
			testutil.NoError(t, err)
			testutil.True(t, strings.HasPrefix(string(sig), "-----BEGIN SSH SIGNATURE-----"))
		}
		testutil.Equal(t, e.calls[gitsign.PrivateKeyRef(gsRef)], 1)
		// The resolver received the live [secrets] config.
		testutil.Equal(t, e.sc[0].Op.BootstrapSource, "keychain://x")
	})

	t.Run("non-git namespace refused", func(t *testing.T) {
		e := newGSEnv("agent-key", gsRef, good)
		_, err := newGitSigner(e.cfgFn, e.resolve)(context.Background(), "t1", "file", []byte("data"))
		testutil.ErrorIs(t, err, gitsign.ErrNamespace)
	})

	t.Run("unresolved then recovered", func(t *testing.T) {
		e := newGSEnv("agent-key", gsRef, map[string]string{})
		f := newGitSigner(e.cfgFn, e.resolve)
		_, err := f(context.Background(), "t1", "git", []byte("data"))
		testutil.ErrorIs(t, err, gitsign.ErrUnresolved)
		e.mu.Lock()
		e.values = good
		e.mu.Unlock()
		_, err = f(context.Background(), "t1", "git", []byte("data"))
		testutil.NoError(t, err)
	})

	t.Run("live policy toggle needs no restart", func(t *testing.T) {
		e := newGSEnv("unsigned", gsRef, good)
		f := newGitSigner(e.cfgFn, e.resolve)
		_, err := f(context.Background(), "t1", "git", []byte("data"))
		testutil.ErrorIs(t, err, errGitSigningOff)
		e.mu.Lock()
		e.cfg.Git.AgentCommitSigning = "agent-key"
		e.mu.Unlock()
		_, err = f(context.Background(), "t1", "git", []byte("data"))
		testutil.NoError(t, err)
		e.mu.Lock()
		e.cfg.Git.AgentCommitSigning = "inherit"
		e.mu.Unlock()
		_, err = f(context.Background(), "t1", "git", []byte("data"))
		testutil.ErrorIs(t, err, errGitSigningOff)
	})

	t.Run("changed key reference gets its own holder", func(t *testing.T) {
		pub2, priv2 := gsKey(t)
		ref2 := "op://claude/Other key"
		vals := map[string]string{
			gitsign.PublicKeyRef(gsRef): pub, gitsign.PrivateKeyRef(gsRef): priv,
			gitsign.PublicKeyRef(ref2): pub2, gitsign.PrivateKeyRef(ref2): priv2,
		}
		e := newGSEnv("agent-key", gsRef, vals)
		f := newGitSigner(e.cfgFn, e.resolve)
		_, err := f(context.Background(), "t1", "git", []byte("data"))
		testutil.NoError(t, err)
		e.mu.Lock()
		e.cfg.Git.AgentSigningKey = ref2
		e.mu.Unlock()
		_, err = f(context.Background(), "t1", "git", []byte("data"))
		testutil.NoError(t, err)
		testutil.Equal(t, e.calls[gitsign.PrivateKeyRef(ref2)], 1)
	})
}
