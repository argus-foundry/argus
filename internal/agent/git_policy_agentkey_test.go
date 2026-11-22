package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/testutil"
)

// testAgentSigningKey returns a synthetic Ed25519 key generated in memory:
// its authorized_keys-form public half and its OpenSSH PEM private half
// (used only to prove the private half never reaches the env).
func testAgentSigningKey(t *testing.T) (pub, priv string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sk, "")
	testutil.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), string(pem.EncodeToMemory(block))
}

// gitConfigGet reads one key through git itself with env as the only
// command-scope source (no system/global config).
func gitConfigGet(t *testing.T, env []string, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git config --get %s: %v: %s", key, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAgentGitPolicy_AgentKey(t *testing.T) {
	pub, priv := testAgentSigningKey(t)
	const program = "/opt/argus/bin/argus"
	sign := AgentSigningEnv{PublicKey: pub, Program: program}
	wantEntries := func(n int, publicKey string) []string {
		idx := func(i int) string { return strconv.Itoa(n + i) }
		return []string{
			"GIT_CONFIG_COUNT=" + strconv.Itoa(n+4),
			"GIT_CONFIG_KEY_" + idx(0) + "=commit.gpgsign", "GIT_CONFIG_VALUE_" + idx(0) + "=true",
			"GIT_CONFIG_KEY_" + idx(1) + "=gpg.format", "GIT_CONFIG_VALUE_" + idx(1) + "=ssh",
			"GIT_CONFIG_KEY_" + idx(2) + "=user.signingkey", "GIT_CONFIG_VALUE_" + idx(2) + "=key::" + publicKey,
			"GIT_CONFIG_KEY_" + idx(3) + "=gpg.ssh.program", "GIT_CONFIG_VALUE_" + idx(3) + "=" + program,
		}
	}
	for _, tc := range []struct {
		name string
		env  []string
		n    int // existing GIT_CONFIG_COUNT
	}{
		{"empty env", nil, 0},
		{"preserve existing entries", []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=Existing"}, 1},
		// A backend mapping or the host can set commit.gpgsign=false first;
		// agent-key's later entry must win.
		{"overrides inherited unsigned", []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]string(nil), tc.env...)
			got, err := agentGitPolicy(tc.env, "agent-key", sign)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, tc.env, original)
			// Existing entries keep order; the four entries come after them.
			testutil.DeepEqual(t, got[:len(tc.env)], tc.env)
			testutil.DeepEqual(t, got[len(tc.env):], wantEntries(tc.n, pub))
			testutil.Equal(t, gitConfigGet(t, got, "commit.gpgsign"), "true")
			testutil.Equal(t, gitConfigGet(t, got, "gpg.format"), "ssh")
			testutil.Equal(t, gitConfigGet(t, got, "user.signingkey"), "key::"+pub)
			testutil.Equal(t, gitConfigGet(t, got, "gpg.ssh.program"), program)
			if tc.name == "preserve existing entries" {
				testutil.Equal(t, gitConfigGet(t, got, "user.name"), "Existing")
			}
			assertNoPrivateKeyMaterial(t, got, priv)
		})
	}
}

func TestAgentGitPolicy_AgentKeyParameters(t *testing.T) {
	pub, priv := testAgentSigningKey(t)
	const program = "/opt/argus/bin/argus"
	env := []string{"GIT_CONFIG_PARAMETERS='commit.gpgsign=false'"}
	got, err := agentGitPolicy(env, "agent-key", AgentSigningEnv{PublicKey: pub, Program: program})
	testutil.NoError(t, err)
	var params []string
	for _, e := range got {
		if v, ok := strings.CutPrefix(e, "GIT_CONFIG_PARAMETERS="); ok {
			params = append(params, v)
		}
	}
	// The inherited value is kept and a final, extended copy is appended
	// (same mechanics as unsigned) so the legacy channel also signs.
	testutil.Equal(t, len(params), 2)
	last := params[len(params)-1]
	testutil.Equal(t, strings.HasPrefix(last, "'commit.gpgsign=false'"), true)
	for _, want := range []string{"'commit.gpgsign=true'", "'gpg.format=ssh'", "'user.signingkey=key::" + pub + "'", "'gpg.ssh.program=" + program + "'"} {
		testutil.Contains(t, last, want)
	}
	testutil.Equal(t, gitConfigGet(t, got, "commit.gpgsign"), "true")
	testutil.Equal(t, gitConfigGet(t, got, "gpg.ssh.program"), program)
	assertNoPrivateKeyMaterial(t, got, priv)
}

// An unresolved public key still routes signing through the helper, so
// commits fail loudly there instead of silently going unsigned.
func TestAgentGitPolicy_AgentKeyEmptyPublicKey(t *testing.T) {
	const program = "/opt/argus/bin/argus"
	got, err := agentGitPolicy(nil, "agent-key", AgentSigningEnv{Program: program})
	testutil.NoError(t, err)
	testutil.Equal(t, gitConfigGet(t, got, "commit.gpgsign"), "true")
	testutil.Equal(t, gitConfigGet(t, got, "gpg.format"), "ssh")
	testutil.Equal(t, gitConfigGet(t, got, "gpg.ssh.program"), program)
}

func TestAgentGitPolicy_AgentKeyBadCount(t *testing.T) {
	pub, _ := testAgentSigningKey(t)
	_, err := agentGitPolicy([]string{"GIT_CONFIG_COUNT=bad"}, "agent-key", AgentSigningEnv{PublicKey: pub, Program: "/x/argus"})
	if err == nil {
		t.Fatal("agent-key with an invalid GIT_CONFIG_COUNT: want error, got nil")
	}
	testutil.Contains(t, err.Error(), "GIT_CONFIG_COUNT")
}

// inherit and unsigned ignore the signing env entirely: output is identical
// with or without one (byte-for-byte unchanged behavior).
func TestAgentGitPolicy_OtherPoliciesIgnoreSigningEnv(t *testing.T) {
	pub, _ := testAgentSigningKey(t)
	sign := AgentSigningEnv{PublicKey: pub, Program: "/opt/argus/bin/argus"}
	env := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=Existing", "GIT_CONFIG_PARAMETERS='a.b=c'"}
	for _, policy := range []string{"", "inherit", "unsigned"} {
		t.Run("policy="+policy, func(t *testing.T) {
			without, err := agentGitPolicy(env, policy, AgentSigningEnv{})
			testutil.NoError(t, err)
			with, err := agentGitPolicy(env, policy, sign)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, with, without)
			for _, e := range with {
				testutil.False(t, strings.Contains(e, pub))
				testutil.False(t, strings.Contains(e, "gpg.ssh.program"))
			}
		})
	}
}

func assertNoPrivateKeyMaterial(t *testing.T, env []string, priv string) {
	t.Helper()
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(priv), "-----BEGIN OPENSSH PRIVATE KEY-----"), "-----END OPENSSH PRIVATE KEY-----"))
	firstLine, _, _ := strings.Cut(body, "\n")
	for _, e := range env {
		testutil.False(t, strings.Contains(e, "PRIVATE KEY"))
		testutil.False(t, strings.Contains(e, firstLine))
	}
}
