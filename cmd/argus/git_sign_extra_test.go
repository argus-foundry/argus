package main

import (
	"bytes"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestGitSignCommand_OptionErrors(t *testing.T) {
	pub, data := writeSignInputs(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unsupported option", []string{"-Y", "sign", "-n", "git", "-Z", pub, data}},
		{"missing -n value", []string{"-Y", "sign", "-f", pub, data, "-n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, nil)
			env, _ := newGitSignEnv(t, srv, &keygenStub{})
			var out, errOut bytes.Buffer
			testutil.NotEqual(t, gitSignCommand(tc.args, env, &out, &errOut), 0)
			testutil.Equal(t, len(*reqs), 0)
			testutil.Contains(t, errOut.String(), coordinatorMsg)
			assertNoSigFile(t, data)
		})
	}
}

// A bare `-Y` has no operation; ssh-keygen owns the usage error.
func TestGitSignCommand_BareYPassesThrough(t *testing.T) {
	srv, _ := fakeSignDaemon(t, nil)
	keygen := &keygenStub{code: 1}
	env, _ := newGitSignEnv(t, srv, keygen)
	var out, errOut bytes.Buffer
	testutil.Equal(t, gitSignCommand([]string{"-Y"}, env, &out, &errOut), 1)
	testutil.DeepEqual(t, keygen.calls, [][]string{{"-Y"}})
}

// A client timeout means the daemon took the request; retrying could stack
// minutes of waiting, so it is final.
func TestGitSignCommand_TimeoutIsFinal(t *testing.T) {
	release := make(chan struct{})
	srv, _ := fakeSignDaemon(t, func(w http.ResponseWriter) { <-release })
	t.Cleanup(func() { close(release) })
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	env.http = &http.Client{Timeout: 50 * time.Millisecond}
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
	testutil.NotEqual(t, code, 0)
	testutil.Equal(t, sl.n, 0)
	testutil.Contains(t, errOut.String(), "timed out")
	assertNoSigFile(t, data)
}

func TestGitSignCommand_NonJSONErrorResponse(t *testing.T) {
	srv, _ := fakeSignDaemon(t, func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) })
	env, _ := newGitSignEnv(t, srv, &keygenStub{})
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	testutil.NotEqual(t, gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut), 0)
	testutil.Contains(t, errOut.String(), "HTTP 502")
}

func TestRealGitSignEnv(t *testing.T) {
	t.Run("override needs both url and token", func(t *testing.T) {
		t.Setenv("ARGUS_GIT_SIGN_URL", "http://127.0.0.1:1")
		t.Setenv("ARGUS_GIT_SIGN_TOKEN", "tok")
		t.Setenv("ARGUS_TASK_ID", "task-x")
		env := realGitSignEnv()
		u, tok, err := env.daemon()
		testutil.NoError(t, err)
		testutil.Equal(t, u, "http://127.0.0.1:1")
		testutil.Equal(t, tok, "tok")
		testutil.Equal(t, env.taskID, "task-x")
		testutil.Equal(t, env.retryFor, 30*time.Second)
		// Outlasts the daemon's worst-case first key resolve (~35s).
		testutil.True(t, env.http.Timeout > 35*time.Second)
	})
	t.Run("url alone falls back to discovery", func(t *testing.T) {
		// Discovery must fail against an empty HOME, never reaching a real
		// daemon or reading the real token.
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ARGUS_GIT_SIGN_URL", "http://127.0.0.1:1")
		t.Setenv("ARGUS_GIT_SIGN_TOKEN", "")
		_, _, err := realGitSignEnv().daemon()
		testutil.Error(t, err)
	})
}

func TestRunSSHKeygen(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not on PATH")
	}
	var out, errOut bytes.Buffer
	// An unknown -Y operation: ssh-keygen exits non-zero and says so.
	code := runSSHKeygen([]string{"-Y", "no-such-op"}, &out, &errOut)
	testutil.NotEqual(t, code, 0)
	testutil.True(t, strings.TrimSpace(errOut.String()) != "")

	t.Setenv("PATH", t.TempDir())
	errOut.Reset()
	testutil.Equal(t, runSSHKeygen([]string{"-Y", "verify"}, &out, &errOut), 255)
	testutil.Contains(t, errOut.String(), "ssh-keygen not found")
}
