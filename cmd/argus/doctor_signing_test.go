package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/doctor"
	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/testutil"
)

const doctorSigningRef = "op://claude/Argus agent signing key"

// doctorSigningKey generates a synthetic Ed25519 key in memory.
func doctorSigningKey(t *testing.T) (pub, priv string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sk, "")
	testutil.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), string(pem.EncodeToMemory(block))
}

func mapResolver(values map[string]string) gitsign.Resolver {
	return func(source string) (string, bool) { v, ok := values[source]; return v, ok }
}

// agentSigningKeyStatusFor takes an explicit config and resolver (like
// secretsBootstrapStatusFor) so the check never touches real 1Password or
// ~/.argus/config.toml in tests.
func TestAgentSigningKeyStatusFor(t *testing.T) {
	pub, priv := doctorSigningKey(t)
	otherPub, _ := doctorSigningKey(t)
	good := map[string]string{gitsign.PublicKeyRef(doctorSigningRef): pub, gitsign.PrivateKeyRef(doctorSigningRef): priv}
	agentKey := config.Config{Git: config.GitConfig{AgentCommitSigning: "agent-key", AgentSigningKey: doctorSigningRef}, API: config.APIConfig{Enabled: true}}
	apiOff := agentKey
	apiOff.API.Enabled = false
	for _, tc := range []struct {
		name      string
		cfg       config.Config
		values    map[string]string
		wantState string
		wantLine  string
	}{
		{"off when inherit", config.Config{Git: config.GitConfig{AgentCommitSigning: "inherit"}}, good, "OFF", "agent signing key: OFF"},
		{"off when unsigned", config.Config{Git: config.GitConfig{AgentCommitSigning: "unsigned", AgentSigningKey: doctorSigningRef}}, good, "OFF", "agent signing key: OFF"},
		{"ok", agentKey, good, "OK", "agent signing key: OK"},
		{"broken unresolved", agentKey, map[string]string{}, "BROKEN", "agent signing key: BROKEN: "},
		{"broken mismatch", agentKey, map[string]string{gitsign.PublicKeyRef(doctorSigningRef): otherPub, gitsign.PrivateKeyRef(doctorSigningRef): priv}, "BROKEN", "agent signing key: BROKEN: "},
		{"broken api disabled", apiOff, good, "BROKEN", "agent signing key: BROKEN: api.enabled is false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := agentSigningKeyStatusFor(tc.cfg, mapResolver(tc.values))
			testutil.Equal(t, st.State, tc.wantState)
			line := doctor.RenderAgentSigningKey(st)
			testutil.Contains(t, line, tc.wantLine)
			// No key material ever reaches doctor's output.
			for _, secret := range []string{pub, otherPub, "PRIVATE KEY", strings.Fields(pub)[1]} {
				testutil.False(t, strings.Contains(line, secret))
			}
		})
	}
}

// Mirrors TestSecretsBootstrapStatusFor_DoesNotAffectExitCodeContract: a
// BROKEN signing key leaves the binary-coherence verdict (doctor's sole
// exit-code input) Healthy.
func TestAgentSigningKeyStatusFor_DoesNotAffectExitCode(t *testing.T) {
	actors := []doctor.Actor{
		{Role: doctor.RolePathArgus, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
		{Role: doctor.RoleArgusdTarget, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
		{Role: doctor.RoleGoInstall, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
		{Role: doctor.RoleDaemon, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
		{Role: doctor.RoleSupervisor, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
		{Role: doctor.RoleTUI, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true},
	}
	cfg := config.Config{Git: config.GitConfig{AgentCommitSigning: "agent-key", AgentSigningKey: doctorSigningRef}}
	testutil.Equal(t, agentSigningKeyStatusFor(cfg, mapResolver(nil)).State, "BROKEN")
	testutil.Equal(t, doctor.Diagnose(actors).Verdict, doctor.Healthy)
}
