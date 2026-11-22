package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// AgentSigningEnv carries what the agent-key policy needs in the child env:
// the agent key's PUBLIC half (authorized_keys form) and the absolute path of
// the argus executable acting as gpg.ssh.program. It never holds private key
// material — the private half stays in the daemon.
type AgentSigningEnv struct {
	PublicKey, Program string
}

// agentGitPolicy appends command-scope Git config to the child environment.
// Existing entries retain order and contents. A legacy GIT_CONFIG_PARAMETERS
// value is parsed by Git after COUNT, so append there too when it is present.
// Neither command-line flags (-S / -c) nor repository files are changed.
//
// unsigned appends commit.gpgsign=false. agent-key appends commit.gpgsign=true,
// gpg.format=ssh, user.signingkey=key::<PublicKey> and
// gpg.ssh.program=<Program>; an empty PublicKey still routes signing through
// the helper so commits fail loudly there instead of going unsigned. Every
// other policy (inherit, "") ignores sign and returns env unchanged.
func agentGitPolicy(env []string, policy string, sign AgentSigningEnv) ([]string, error) {
	var entries [][2]string
	switch policy {
	case config.GitSigningUnsigned:
		entries = [][2]string{{"commit.gpgsign", "false"}}
	case config.GitSigningAgentKey:
		entries = [][2]string{
			{"commit.gpgsign", "true"},
			{"gpg.format", "ssh"},
			{"user.signingkey", "key::" + sign.PublicKey},
			{"gpg.ssh.program", sign.Program},
		}
	default:
		return env, nil
	}
	var count, parameters string
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		switch name {
		case "GIT_CONFIG_COUNT":
			count = value
		case "GIT_CONFIG_PARAMETERS":
			parameters = value
		}
	}
	n := 0
	if count != "" {
		parsed, err := strconv.Atoi(count)
		if err != nil || parsed < 0 || parsed > int(^uint(0)>>1)-len(entries) {
			return nil, fmt.Errorf("agent Git policy: invalid GIT_CONFIG_COUNT")
		}
		n = parsed
	}
	out := append([]string(nil), env...)
	out = append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n+len(entries)))
	for i, kv := range entries {
		idx := strconv.Itoa(n + i)
		out = append(out, "GIT_CONFIG_KEY_"+idx+"="+kv[0], "GIT_CONFIG_VALUE_"+idx+"="+kv[1])
	}
	if parameters != "" {
		extended := parameters
		for _, kv := range entries {
			extended += " " + gitConfigParameter(kv[0]+"="+kv[1])
		}
		out = append(out, "GIT_CONFIG_PARAMETERS="+extended)
	}
	return out, nil
}

// gitConfigParameter single-quotes s the way Git's sq_quote does for
// GIT_CONFIG_PARAMETERS: each embedded quote closes the string, emits an
// escaped quote, and reopens it.
func gitConfigParameter(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// resolveAgentSigningPublicKey resolves a secret source (always the agent
// key's "/public key" field) for agentSigningEnvFor. A test seam; production
// is the per-backend secret path (op:// via the service-account bootstrap,
// success-only memoized).
var resolveAgentSigningPublicKey = resolveBackendEnvSource

// argusExecutable returns the absolute path of the running argus binary,
// which Git invokes as gpg.ssh.program. Symlinks are deliberately NOT
// resolved: a symlink-resolved, version-pinned target can vanish on upgrade,
// while the invoked path keeps pointing at the installed argus. A test seam.
var argusExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

// unavailableSigningHelper is gpg.ssh.program when the argus executable cannot
// be located. It never resolves on PATH, so every commit fails loudly with a
// self-describing name instead of running a possibly stale argus from PATH.
const unavailableSigningHelper = "argus-signing-helper-unavailable"

// agentSigningEnvFor builds the agent-key signing env for one spawn. Failures
// never abort the spawn: Program is still set so commits fail loudly inside
// the helper with a named cause instead of silently going unsigned. Only the
// public half is ever resolved here.
func agentSigningEnvFor(cfg config.Config, taskID string) AgentSigningEnv {
	var sign AgentSigningEnv
	if exe, err := argusExecutable(); err != nil {
		uxlog.Log("[agent-git] task %q: cannot locate argus executable for gpg.ssh.program: %v", taskID, err)
		sign.Program = unavailableSigningHelper
	} else {
		sign.Program = exe
	}
	pub, ok := resolveAgentSigningPublicKey(cfg, gitsign.PublicKeyRef(strings.TrimSpace(cfg.Git.AgentSigningKey)))
	pub = strings.TrimSpace(pub)
	if !ok || pub == "" {
		uxlog.Log("[agent-git] task %q: agent signing public key did not resolve; commits will fail in the signing helper", taskID)
		return sign
	}
	sign.PublicKey = pub
	return sign
}
