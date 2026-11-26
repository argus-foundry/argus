package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/gitsign"
	"github.com/argus-foundry/argus/internal/testutil"
)

// helperKey is the session's agent key for helper tests: the -f file holds
// its public half and the fake daemon signs with it, because the helper
// verifies every signature against the -f key before writing it.
var helperKey = func() ssh.Signer {
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	s, err := ssh.NewSignerFromKey(sk)
	if err != nil {
		panic(err)
	}
	return s
}()

// helperSig is the (deterministic, Ed25519) signature the fake daemon
// returns for helperData.
var helperSig = func() string {
	sig, err := gitsign.Sign(helperKey, "git", []byte(helperData))
	if err != nil {
		panic(err)
	}
	return string(sig)
}()

const (
	helperData     = "tree 4b825dc6\nauthor agent\n\ncommit body\n"
	helperTaskID   = "task-sign-1"
	helperToken    = "daemon-master-token"
	coordinatorMsg = "report this to your coordinator"
)

// signRequest is what the fake daemon decoded from the helper's POST.
type signRequest struct {
	path, auth, namespace, taskID string
	data                          []byte
}

// fakeSignDaemon emulates POST /api/git/sign. respond == nil answers 200
// with helperSig.
func fakeSignDaemon(t *testing.T, respond func(w http.ResponseWriter)) (*httptest.Server, *[]signRequest) {
	t.Helper()
	var reqs []signRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Namespace string `json:"namespace"`
			Data      string `json:"data"`
			TaskID    string `json:"task_id"`
		}
		// Handler goroutine: report with Errorf, never Fatal.
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("fake daemon: decode body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, err := base64.StdEncoding.DecodeString(body.Data)
		if err != nil {
			t.Errorf("fake daemon: data is not std base64: %v", err)
		}
		reqs = append(reqs, signRequest{r.Method + " " + r.URL.Path, r.Header.Get("Authorization"), body.Namespace, body.TaskID, data})
		if respond != nil {
			respond(w)
			return
		}
		writeJSONTest(w, http.StatusOK, map[string]string{"signature": helperSig})
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// keygenStub records pass-through invocations of the real ssh-keygen.
type keygenStub struct {
	calls [][]string
	code  int
}

func (k *keygenStub) run(args []string, stdout, stderr io.Writer) int {
	k.calls = append(k.calls, append([]string(nil), args...))
	_, _ = io.WriteString(stdout, "keygen-stdout")
	_, _ = io.WriteString(stderr, "keygen-stderr")
	return k.code
}

// sleepRecorder is the injected sleep: it records and never waits. It
// aborts the test if the helper keeps sleeping far past retryFor, so a
// retry loop that ignores the budget fails instead of hanging.
type sleepRecorder struct {
	t     *testing.T
	limit time.Duration
	total time.Duration
	n     int
}

func (s *sleepRecorder) sleep(d time.Duration) {
	s.n++
	s.total += d
	if s.total > 2*s.limit || s.n > 10000 {
		s.t.Fatalf("helper kept retrying: %d sleeps totalling %v (retryFor %v)", s.n, s.total, s.limit)
	}
}

func newGitSignEnv(t *testing.T, srv *httptest.Server, keygen *keygenStub) (gitSignEnv, *sleepRecorder) {
	t.Helper()
	sl := &sleepRecorder{t: t, limit: 30 * time.Second}
	return gitSignEnv{
		daemon:    func() (string, string, error) { return srv.URL, helperToken, nil },
		http:      srv.Client(),
		sshKeygen: keygen.run,
		retryFor:  30 * time.Second,
		sleep:     sl.sleep,
		taskID:    helperTaskID,
	}, sl
}

// writeSignInputs writes the public-key file and data buffer git hands
// the helper. Only public material is written.
func writeSignInputs(t *testing.T) (pubPath, dataPath string) {
	t.Helper()
	dir := t.TempDir()
	pubPath = filepath.Join(dir, "signing_key.pub")
	dataPath = filepath.Join(dir, "buffer")
	testutil.NoError(t, os.WriteFile(pubPath, ssh.MarshalAuthorizedKey(helperKey.PublicKey()), 0o600))
	testutil.NoError(t, os.WriteFile(dataPath, []byte(helperData), 0o600))
	return pubPath, dataPath
}

func assertNoSigFile(t *testing.T, dataPath string) {
	t.Helper()
	_, err := os.Stat(dataPath + ".sig")
	testutil.True(t, errors.Is(err, os.ErrNotExist))
}

func TestGitSignCommand_SignWritesSig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra func(pub, data string) []string
	}{
		{"plain", func(pub, data string) []string { return []string{"-Y", "sign", "-n", "git", "-f", pub, data} }},
		// git passes -U for a literal key:: signing key; -O options are ignored.
		{"with -U", func(pub, data string) []string { return []string{"-Y", "sign", "-n", "git", "-f", pub, "-U", data} }},
		{"with -O", func(pub, data string) []string {
			return []string{"-Y", "sign", "-n", "git", "-O", "hashalg=sha512", "-f", pub, data}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, nil)
			keygen := &keygenStub{}
			env, sl := newGitSignEnv(t, srv, keygen)
			pub, data := writeSignInputs(t)
			var out, errOut bytes.Buffer
			code := gitSignCommand(tc.extra(pub, data), env, &out, &errOut)
			testutil.Equal(t, code, 0)
			sig, err := os.ReadFile(data + ".sig")
			testutil.NoError(t, err)
			testutil.Equal(t, string(sig), helperSig)
			if len(*reqs) != 1 {
				t.Fatalf("daemon requests = %d, want 1", len(*reqs))
			}
			got := (*reqs)[0]
			testutil.Equal(t, got.path, "POST /api/git/sign")
			testutil.Equal(t, got.auth, "Bearer "+helperToken)
			testutil.Equal(t, got.namespace, "git")
			testutil.Equal(t, got.taskID, helperTaskID)
			testutil.Equal(t, string(got.data), helperData)
			testutil.Equal(t, len(keygen.calls), 0)
			testutil.Equal(t, sl.n, 0)
		})
	}
}

func TestGitSignCommand_NonSignPassesThrough(t *testing.T) {
	for _, args := range [][]string{
		{"-Y", "verify", "-n", "git", "-f", "/tmp/allowed", "-I", "agent@argus", "-s", "/tmp/x.sig"},
		{"-Y", "find-principals", "-f", "/tmp/allowed", "-s", "/tmp/x.sig"},
		{"-Y", "check-novalidate", "-n", "git", "-s", "/tmp/x.sig"},
	} {
		t.Run(args[1], func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, nil)
			keygen := &keygenStub{code: 255}
			env, _ := newGitSignEnv(t, srv, keygen)
			var out, errOut bytes.Buffer
			code := gitSignCommand(args, env, &out, &errOut)
			// ssh-keygen's result is returned unchanged.
			testutil.Equal(t, code, 255)
			testutil.DeepEqual(t, keygen.calls, [][]string{args})
			testutil.Equal(t, out.String(), "keygen-stdout")
			testutil.Equal(t, errOut.String(), "keygen-stderr")
			testutil.Equal(t, len(*reqs), 0)
		})
	}
}

// flakyTransport fails the first `failures` round trips with a connection
// error, then delegates.
type flakyTransport struct {
	failures int32
	n        atomic.Int32
	next     http.RoundTripper
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.n.Add(1) <= f.failures {
		return nil, &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	}
	return f.next.RoundTrip(r)
}

func TestGitSignCommand_RetriesConnectionFailure(t *testing.T) {
	srv, reqs := fakeSignDaemon(t, nil)
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	ft := &flakyTransport{failures: 3, next: srv.Client().Transport}
	env.http = &http.Client{Transport: ft}
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
	testutil.Equal(t, code, 0)
	sig, err := os.ReadFile(data + ".sig")
	testutil.NoError(t, err)
	testutil.Equal(t, string(sig), helperSig)
	testutil.Equal(t, len(*reqs), 1)
	testutil.True(t, sl.n >= 1)
	testutil.True(t, sl.total <= env.retryFor)
}

// A daemon bounce also makes discovery (socket → port) fail briefly; that
// is a connection failure too and is retried.
func TestGitSignCommand_RetriesDiscoveryFailure(t *testing.T) {
	srv, reqs := fakeSignDaemon(t, nil)
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	var calls atomic.Int32
	env.daemon = func() (string, string, error) {
		if calls.Add(1) <= 2 {
			return "", "", errors.New("dial unix daemon.sock: connect: no such file or directory")
		}
		return srv.URL, helperToken, nil
	}
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
	testutil.Equal(t, code, 0)
	testutil.Equal(t, len(*reqs), 1)
	testutil.True(t, sl.n >= 1)
	testutil.True(t, sl.total <= env.retryFor)
}

func TestGitSignCommand_ConnectionRetryExhausted(t *testing.T) {
	srv, reqs := fakeSignDaemon(t, nil)
	env, sl := newGitSignEnv(t, srv, &keygenStub{})
	env.http = &http.Client{Transport: &flakyTransport{failures: 1 << 30, next: srv.Client().Transport}}
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
	testutil.NotEqual(t, code, 0)
	testutil.Equal(t, len(*reqs), 0)
	// Gave up once the 30s budget was spent, not before and not long after.
	testutil.True(t, sl.total >= env.retryFor-5*time.Second)
	testutil.True(t, sl.total <= env.retryFor+5*time.Second)
	testutil.Contains(t, errOut.String(), coordinatorMsg)
	testutil.Contains(t, errOut.String(), "do not retry unsigned")
	assertNoSigFile(t, data)
}

func TestGitSignCommand_DaemonErrorIsFinal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		cause  string
	}{
		{"key unresolved", http.StatusServiceUnavailable, "agent signing key unresolved"},
		{"key mismatch", http.StatusServiceUnavailable, "agent signing key mismatch"},
		{"bad namespace", http.StatusBadRequest, "namespace must be git"},
		{"server error", http.StatusInternalServerError, "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, func(w http.ResponseWriter) {
				writeJSONTest(w, tc.status, map[string]string{"error": tc.cause})
			})
			env, sl := newGitSignEnv(t, srv, &keygenStub{})
			pub, data := writeSignInputs(t)
			var out, errOut bytes.Buffer
			code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
			testutil.NotEqual(t, code, 0)
			testutil.Equal(t, len(*reqs), 1)
			testutil.Equal(t, sl.n, 0)
			testutil.Contains(t, errOut.String(), tc.cause)
			testutil.Contains(t, errOut.String(), coordinatorMsg)
			testutil.Contains(t, errOut.String(), "do not retry unsigned")
			assertNoSigFile(t, data)
		})
	}
}

func TestGitSignCommand_EmptySignatureFails(t *testing.T) {
	srv, _ := fakeSignDaemon(t, func(w http.ResponseWriter) {
		writeJSONTest(w, http.StatusOK, map[string]string{"signature": ""})
	})
	env, _ := newGitSignEnv(t, srv, &keygenStub{})
	pub, data := writeSignInputs(t)
	var out, errOut bytes.Buffer
	code := gitSignCommand([]string{"-Y", "sign", "-n", "git", "-f", pub, data}, env, &out, &errOut)
	testutil.NotEqual(t, code, 0)
	testutil.Contains(t, errOut.String(), coordinatorMsg)
	assertNoSigFile(t, data)
}

func TestGitSignCommand_LocalFailures(t *testing.T) {
	pub, data := writeSignInputs(t)
	missing := filepath.Join(t.TempDir(), "missing-buffer")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing data file", []string{"-Y", "sign", "-n", "git", "-f", pub, missing}},
		{"no data file arg", []string{"-Y", "sign", "-n", "git", "-f", pub}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := fakeSignDaemon(t, nil)
			keygen := &keygenStub{}
			env, _ := newGitSignEnv(t, srv, keygen)
			var out, errOut bytes.Buffer
			code := gitSignCommand(tc.args, env, &out, &errOut)
			testutil.NotEqual(t, code, 0)
			testutil.Equal(t, len(*reqs), 0)
			testutil.Equal(t, len(keygen.calls), 0)
			testutil.NotEqual(t, errOut.String(), "")
			assertNoSigFile(t, missing)
			assertNoSigFile(t, data)
		})
	}
}
