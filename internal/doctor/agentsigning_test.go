package doctor

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/testutil"
)

// TestRenderAgentSigningKey pins the advisory `argus doctor` line for the
// agent signing key (add-agent-signing-key). doctor has no shared report
// struct, so the check follows the RenderSecretsBootstrap pattern: one
// Render function per advisory section.
func TestRenderAgentSigningKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   gitsign.Status
		want string
	}{
		{"off", gitsign.Status{State: "OFF"}, "agent signing key: OFF"},
		{"ok", gitsign.Status{State: "OK"}, "agent signing key: OK"},
		{"broken", gitsign.Status{State: "BROKEN", Cause: "agent signing key unresolved: op://claude/k/private key"}, "agent signing key: BROKEN: agent signing key unresolved: op://claude/k/private key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderAgentSigningKey(tc.st)
			testutil.Contains(t, got, tc.want)
			testutil.True(t, strings.HasSuffix(got, "\n"))
			if tc.st.State != "BROKEN" {
				testutil.False(t, strings.Contains(got, "BROKEN"))
			}
		})
	}
}

// The line is advisory: the binary-coherence verdict (the only input to
// doctor's exit code) cannot see it.
func TestRenderAgentSigningKey_DoesNotAffectVerdict(t *testing.T) {
	actors := healthyActorsForSigning()
	testutil.Equal(t, Diagnose(actors).Verdict, Healthy)
	_ = RenderAgentSigningKey(gitsign.Status{State: "BROKEN", Cause: "agent signing key mismatch"})
	testutil.Equal(t, Diagnose(actors).Verdict, Healthy)
}

func healthyActorsForSigning() []Actor {
	var out []Actor
	for _, r := range []Role{RolePathArgus, RoleArgusdTarget, RoleGoInstall, RoleDaemon, RoleSupervisor, RoleTUI} {
		out = append(out, Actor{Role: r, ResolvedPath: "/opt/bin/argus", Hash: "h", Resolved: true})
	}
	return out
}
