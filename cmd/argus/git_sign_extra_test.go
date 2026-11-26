package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/gitsign"
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

// The helper verifies the daemon's signature against the -f key before
// writing it: a daemon holding a rotated-out key (or a hostile endpoint)
// fails the commit instead of producing a bad .sig.
func TestGitSignCommand_RejectsSignatureFromOtherKey(t *testing.T) {
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	other, err := ssh.NewSignerFromKey(sk)
	testutil.NoError(t, err)
	otherSig, err := gitsign.Sign(other, "git", []byte(helperData))
	testutil.NoError(t, err)
	for _, tc := range []struct {
		name string
		sig  string
	}{
		{"different key", string(otherSig)},
		{"not a signature", "-----BEGIN SSH SIGNATURE-----\nU1NIU0lHZmFrZQ==\n-----END SSH SIGNATURE-----\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := fakeSignDaemon(t, func(w http.ResponseWriter) {
				writeJSONTest(w, http.StatusOK, map[string]string{"signature": tc.sig})
			})
			env, _ := newGitSignEnv(t, srv, &keygenStub{})
			pub, data := writeSignInputs(t)
			var out, errOut bytes.Buffer
			testutil.NotEqual(t, gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut), 0)
			testutil.Contains(t, errOut.String(), "signature was not made by the session's configured key")
			testutil.Contains(t, errOut.String(), coordinatorMsg)
			assertNoSigFile(t, data)
		})
	}
}

func TestGitSignCommand_BadKeyFile(t *testing.T) {
	_, data := writeSignInputs(t)
	garbage := filepath.Join(t.TempDir(), "k.pub")
	testutil.NoError(t, os.WriteFile(garbage, []byte("not a key\n"), 0o600))
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no -f", []string{"-Y", "sign", "-n", "git", data}},
		{"missing -f file", []string{"-Y", "sign", "-n", "git", "-f", filepath.Join(t.TempDir(), "nope"), data}},
		{"unparseable -f file", []string{"-Y", "sign", "-n", "git", "-f", garbage, data}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, nil)
			env, _ := newGitSignEnv(t, srv, &keygenStub{})
			var out, errOut bytes.Buffer
			testutil.NotEqual(t, gitSignCommand(tc.args, env, &out, &errOut), 0)
			testutil.Equal(t, len(*reqs), 0)
			assertNoSigFile(t, data)
		})
	}
}

func TestGitSignCommand_MultipleDataFiles(t *testing.T) {
	pub, data := writeSignInputs(t)
	srv, reqs := fakeSignDaemon(t, nil)
	env, _ := newGitSignEnv(t, srv, &keygenStub{})
	var out, errOut bytes.Buffer
	testutil.NotEqual(t, gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data, data}, env, &out, &errOut), 0)
	testutil.Equal(t, len(*reqs), 0)
	testutil.Contains(t, errOut.String(), "exactly one data file")
	assertNoSigFile(t, data)
}

// api.enabled = false cannot be fixed by waiting: fail at once, named.
func TestGitSignCommand_APIDisabledIsFinal(t *testing.T) {
	srv, reqs := fakeSignDaemon(t, nil)
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	env.daemon = func() (string, string, error) { return "", "", fmt.Errorf("discover: %w", errAPIDisabled) }
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	testutil.NotEqual(t, gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut), 0)
	testutil.Equal(t, sl.n, 0)
	testutil.Equal(t, len(*reqs), 0)
	testutil.Contains(t, errOut.String(), "api.enabled is false")
	assertNoSigFile(t, data)
}

// A daemon that accepts the socket connection but never answers discovery
// must not hang `git commit`.
func TestDiscoverAPIPort_WedgedDaemonTimesOut(t *testing.T) {
	home, err := os.MkdirTemp("", "gs") // short: macOS unix socket paths cap at 104 bytes
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	testutil.NoError(t, os.MkdirAll(filepath.Join(home, ".argus"), 0o700))
	ln, err := net.Listen("unix", filepath.Join(home, ".argus", "daemon.sock"))
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() }) // hold it open, never answer
		}
	}()
	old := discoverAPIPortTimeout
	discoverAPIPortTimeout = 100 * time.Millisecond
	t.Cleanup(func() { discoverAPIPortTimeout = old })

	done := make(chan error, 1)
	go func() { _, err := discoverAPIPort(); done <- err }()
	select {
	case err := <-done:
		testutil.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("discoverAPIPort hung on a wedged daemon")
	}
}

// A discovery timeout means a wedged daemon, not a bounce: final.
func TestGitSignCommand_DiscoveryTimeoutIsFinal(t *testing.T) {
	srv, _ := fakeSignDaemon(t, nil)
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	env.daemon = func() (string, string, error) {
		return "", "", fmt.Errorf("Daemon.Ports: %w", os.ErrDeadlineExceeded)
	}
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	testutil.NotEqual(t, gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut), 0)
	testutil.Equal(t, sl.n, 0)
	assertNoSigFile(t, data)
}
