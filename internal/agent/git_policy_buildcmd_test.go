package agent

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const testAgentKeyRef = "op://claude/Argus agent signing key"

// stubAgentSigning swaps the public-key resolver and executable seams. The
// resolver hands out the private half for any source other than the public
// field, so a code path that ever asked for it would leak the sentinel.
func stubAgentSigning(t *testing.T, pub, priv string, ok bool, exe string) *[]string {
	t.Helper()
	var sources []string
	prevResolve, prevExe := resolveAgentSigningPublicKey, argusExecutable
	resolveAgentSigningPublicKey = func(_ config.Config, source string) (string, bool) {
		sources = append(sources, source)
		if source == testAgentKeyRef+"/public key" {
			return pub, ok
		}
		return priv, true
	}
	argusExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { resolveAgentSigningPublicKey, argusExecutable = prevResolve, prevExe })
	return &sources
}

func agentKeyCmdEnv(t *testing.T, cfg config.Config) []string {
	t.Helper()
	cmd, cleanup, err := BuildCmd(&model.Task{ID: "agent-key", Backend: "bare", Worktree: t.TempDir()}, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return append(append([]string(nil), cmd.Env...), cmd.Args...)
}

func TestBuildCmd_AgentKeySigningEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, priv := testAgentSigningKey(t)
	const exe = "/opt/argus/bin/argus"
	sources := stubAgentSigning(t, pub, priv, true, exe)
	logText := captureUXLog(t)
	cfg := testConfig()
	cfg.Git = config.GitConfig{AgentCommitSigning: config.GitSigningAgentKey, AgentSigningKey: testAgentKeyRef}

	got := agentKeyCmdEnv(t, cfg)

	testutil.DeepEqual(t, *sources, []string{testAgentKeyRef + "/public key"})
	testutil.Equal(t, gitConfigGet(t, got, "commit.gpgsign"), "true")
	testutil.Equal(t, gitConfigGet(t, got, "gpg.format"), "ssh")
	testutil.Equal(t, gitConfigGet(t, got, "user.signingkey"), "key::"+pub)
	testutil.Equal(t, gitConfigGet(t, got, "gpg.ssh.program"), exe)
	assertNoPrivateKeyMaterial(t, got, priv)
	log := logText()
	testutil.Contains(t, log, "policy=agent-key public_key_resolved=true")
	testutil.False(t, strings.Contains(log, pub))
}

// An unresolved public key still points gpg.ssh.program at the helper so the
// commit fails loudly there, and the spawn itself succeeds.
func TestBuildCmd_AgentKeyUnresolvedStillRoutesToHelper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, priv := testAgentSigningKey(t)
	const exe = "/opt/argus/bin/argus"
	stubAgentSigning(t, "", priv, false, exe)
	logText := captureUXLog(t)
	cfg := testConfig()
	cfg.Git = config.GitConfig{AgentCommitSigning: config.GitSigningAgentKey, AgentSigningKey: testAgentKeyRef}

	got := agentKeyCmdEnv(t, cfg)

	testutil.Equal(t, gitConfigGet(t, got, "commit.gpgsign"), "true")
	testutil.Equal(t, gitConfigGet(t, got, "gpg.ssh.program"), exe)
	assertNoPrivateKeyMaterial(t, got, priv)
	log := logText()
	testutil.Contains(t, log, "public_key_resolved=false")
	testutil.Contains(t, log, "did not resolve")
}

// A failed executable lookup still names a helper rather than dropping signing.
func TestAgentSigningEnvFor_ExecutableError(t *testing.T) {
	prevResolve, prevExe := resolveAgentSigningPublicKey, argusExecutable
	t.Cleanup(func() { resolveAgentSigningPublicKey, argusExecutable = prevResolve, prevExe })
	resolveAgentSigningPublicKey = func(config.Config, string) (string, bool) { return " ssh-ed25519 AAAA ", true }
	argusExecutable = func() (string, error) { return "", errors.New("no exe") }
	logText := captureUXLog(t)
	got := agentSigningEnvFor(config.Config{Git: config.GitConfig{AgentSigningKey: testAgentKeyRef}}, "t")
	// An unresolvable, self-describing program fails every commit loudly
	// rather than running whatever argus happens to be on PATH.
	testutil.Equal(t, got, AgentSigningEnv{PublicKey: "ssh-ed25519 AAAA", Program: "argus-signing-helper-unavailable"})
	testutil.Contains(t, logText(), `task "t": cannot locate argus executable for gpg.ssh.program: no exe`)
}

func TestArgusExecutable_Absolute(t *testing.T) {
	exe, err := argusExecutable()
	testutil.NoError(t, err)
	testutil.Equal(t, filepath.IsAbs(exe), true)
}

// inherit and unsigned never consult the agent key (even with a stray key
// configured) and produce exactly the env they did without one.
func TestBuildCmd_NonAgentKeyPoliciesUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub, priv := testAgentSigningKey(t)
	for _, policy := range []string{"", config.GitSigningInherit, config.GitSigningUnsigned} {
		t.Run("policy="+policy, func(t *testing.T) {
			sources := stubAgentSigning(t, pub, priv, true, "/opt/argus/bin/argus")
			worktree := t.TempDir()
			build := func(key string) []string {
				cfg := testConfig()
				cfg.Git = config.GitConfig{AgentCommitSigning: policy, AgentSigningKey: key}
				cmd, cleanup, err := BuildCmd(&model.Task{ID: "plain", Backend: "bare", Worktree: worktree}, cfg, false)
				testutil.NoError(t, err)
				if cleanup != nil {
					t.Cleanup(cleanup)
				}
				// The bridge generation is a fresh random ID per spawn.
				var env []string
				for _, e := range cmd.Env {
					if !strings.HasPrefix(e, "ARGUS_BRIDGE_GENERATION=") {
						env = append(env, e)
					}
				}
				return env
			}
			withKey := build(testAgentKeyRef)
			testutil.DeepEqual(t, withKey, build(""))
			testutil.Equal(t, len(*sources), 0)
			for _, e := range withKey {
				testutil.False(t, strings.Contains(e, "gpg.ssh.program"))
			}
		})
	}
}
