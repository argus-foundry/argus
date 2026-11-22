package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/testutil"
)

// TestGitSign_EndToEnd proves the whole chain with real tools: a real
// `git commit` whose gpg.ssh.program is a freshly built argus binary, signing
// through an in-test /api/git/sign backed by a real gitsign.KeyHolder over an
// in-memory key, then `git verify-commit` (which runs the helper's ssh-keygen
// passthrough) against an allowed_signers file holding only the public key.
// No live daemon, no ~/.argus, no 1Password.
func TestGitSign_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the argus binary and runs real git")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not on PATH")
	}

	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sk, "")
	testutil.NoError(t, err)
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	const ref = "op://claude/e2e key"
	holder := gitsign.NewKeyHolder(func(src string) (string, bool) {
		switch src {
		case gitsign.PublicKeyRef(ref):
			return pub, true
		case gitsign.PrivateKeyRef(ref):
			return string(pem.EncodeToMemory(block)), true
		}
		return "", false
	}, ref)

	const token = "e2e-token"
	var signed atomic.Int32
	var gotTask atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/git/sign" || r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Namespace string `json:"namespace"`
			Data      string `json:"data"`
			TaskID    string `json:"task_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, err := base64.StdEncoding.DecodeString(body.Data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s, err := holder.Signer()
		if err != nil {
			writeJSONTest(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		sig, err := gitsign.Sign(s, body.Namespace, data)
		if err != nil {
			writeJSONTest(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		signed.Add(1)
		gotTask.Store(body.TaskID)
		writeJSONTest(w, http.StatusOK, map[string]string{"signature": string(sig)})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	argusBin := filepath.Join(dir, "argus")
	build := exec.Command("go", "build", "-o", argusBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	repo := filepath.Join(dir, "repo")
	testutil.NoError(t, os.MkdirAll(repo, 0o755))
	home := filepath.Join(dir, "home")
	testutil.NoError(t, os.MkdirAll(home, 0o755))
	allowed := filepath.Join(dir, "allowed_signers")
	testutil.NoError(t, os.WriteFile(allowed, []byte("agent@argus "+pub+"\n"), 0o600))

	env := append(os.Environ(),
		"HOME="+home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"ARGUS_GIT_SIGN_URL="+srv.URL,
		"ARGUS_GIT_SIGN_TOKEN="+token,
		"ARGUS_TASK_ID=e2e-task",
	)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBin, append([]string{
			"-c", "user.name=Agent", "-c", "user.email=agent@argus",
			// The same four entries agentGitPolicy exports for agent-key.
			"-c", "commit.gpgsign=true", "-c", "gpg.format=ssh",
			"-c", "user.signingkey=key::" + pub, "-c", "gpg.ssh.program=" + argusBin,
			"-c", "gpg.ssh.allowedSignersFile=" + allowed,
		}, args...)...)
		cmd.Dir = repo
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello\n"), 0o600))
	git("add", "f.txt")
	git("commit", "-q", "-m", "signed by the agent key")

	testutil.Equal(t, signed.Load(), int32(1))
	testutil.Equal(t, gotTask.Load(), any("e2e-task"))
	testutil.Contains(t, git("cat-file", "commit", "HEAD"), "-----BEGIN SSH SIGNATURE-----")
	out := git("verify-commit", "-v", "HEAD")
	testutil.Contains(t, out, `Good "git" signature for agent@argus`)
	logOut := git("log", "--show-signature", "-1")
	testutil.Contains(t, logOut, `Good "git" signature for agent@argus`)
	t.Logf("verify-commit:\n%s\nlog --show-signature:\n%s", out, logOut)
}
