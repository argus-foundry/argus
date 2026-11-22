package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	gitSignData      = "tree 4b825dc6\nauthor agent-commit-payload-marker\n"
	gitSignSignature = "-----BEGIN SSH SIGNATURE-----\nU1NIU0lHZmFrZQ==\n-----END SSH SIGNATURE-----\n"
)

// gitSignCall records what the handler passed to the signer.
type gitSignCall struct {
	taskID, namespace string
	data              []byte
}

// testGitSignAPI builds the authenticated route tree. A nil sign leaves
// the signer unset (the policy-off / not-wired case).
func testGitSignAPI(t *testing.T, sign GitSignFunc) http.Handler {
	t.Helper()
	srv, _ := testServer(t)
	if sign != nil {
		srv.SetGitSigner(sign)
	}
	return authMiddleware(srv.token, srv.db, srv.push, srv.routes(),
		"/", "/share", "/vendor/", "/manifest.webmanifest", "/sw.js")
}

func doGitSign(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/git/sign", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func gitSignBody(t *testing.T, namespace, data, taskID string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"namespace": namespace,
		"data":      base64.StdEncoding.EncodeToString([]byte(data)),
		"task_id":   taskID,
	})
	testutil.NoError(t, err)
	return string(b)
}

func recordingSigner(sig []byte, err error) (GitSignFunc, *[]gitSignCall, *atomic.Int32) {
	var calls []gitSignCall
	var n atomic.Int32
	return func(_ context.Context, taskID, namespace string, data []byte) ([]byte, error) {
		n.Add(1)
		calls = append(calls, gitSignCall{taskID, namespace, append([]byte(nil), data...)})
		return sig, err
	}, &calls, &n
}

func errorField(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Error
}

func TestGitSign_Success(t *testing.T) {
	sign, calls, n := recordingSigner([]byte(gitSignSignature), nil)
	h := testGitSignAPI(t, sign)
	w := doGitSign(t, h, "test-token", gitSignBody(t, "git", gitSignData, "task-42"))
	testutil.Equal(t, w.Code, http.StatusOK)
	var got struct {
		Signature string `json:"signature"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Signature, gitSignSignature)
	if n.Load() != 1 {
		t.Fatalf("signer calls = %d, want 1", n.Load())
	}
	call := (*calls)[0]
	testutil.Equal(t, call.taskID, "task-42")
	testutil.Equal(t, call.namespace, "git")
	testutil.Equal(t, string(call.data), gitSignData)
}

func TestGitSign_RequiresAuth(t *testing.T) {
	sign, _, n := recordingSigner([]byte(gitSignSignature), nil)
	h := testGitSignAPI(t, sign)
	for _, token := range []string{"", "wrong-token"} {
		t.Run("token="+token, func(t *testing.T) {
			w := doGitSign(t, h, token, gitSignBody(t, "git", gitSignData, "task-42"))
			testutil.Equal(t, w.Code, http.StatusUnauthorized)
			testutil.False(t, strings.Contains(w.Body.String(), "SSH SIGNATURE"))
		})
	}
	testutil.Equal(t, n.Load(), int32(0))
}

func TestGitSign_SignerUnset(t *testing.T) {
	h := testGitSignAPI(t, nil)
	w := doGitSign(t, h, "test-token", gitSignBody(t, "git", gitSignData, "task-42"))
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	// A named cause, not a bare status.
	msg := errorField(t, w)
	testutil.Contains(t, msg, "sign")
}

func TestGitSign_KeyErrorIs503WithCause(t *testing.T) {
	sign, _, _ := recordingSigner(nil, errors.New("agent signing key unresolved: op://claude/k/private key"))
	h := testGitSignAPI(t, sign)
	w := doGitSign(t, h, "test-token", gitSignBody(t, "git", gitSignData, "task-42"))
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	testutil.Contains(t, errorField(t, w), "unresolved")
}

func TestGitSign_BadRequests(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"namespace file", gitSignBody(t, "file", gitSignData, "task-42")},
		{"namespace empty", gitSignBody(t, "", gitSignData, "task-42")},
		{"not json", "namespace=git"},
		{"data not base64", `{"namespace":"git","data":"%%%not-base64%%%","task_id":"task-42"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sign, _, n := recordingSigner([]byte(gitSignSignature), nil)
			h := testGitSignAPI(t, sign)
			w := doGitSign(t, h, "test-token", tc.body)
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.NotEqual(t, errorField(t, w), "")
			// Refused before the key is ever used: no signature produced.
			testutil.Equal(t, n.Load(), int32(0))
			testutil.False(t, strings.Contains(w.Body.String(), "SSH SIGNATURE"))
		})
	}
}

// Every request logs its task ID and outcome, never the data, its base64
// form, or the signature.
func TestGitSign_LogsTaskAndOutcomeOnly(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)

	okSign, _, _ := recordingSigner([]byte(gitSignSignature), nil)
	w := doGitSign(t, testGitSignAPI(t, okSign), "test-token", gitSignBody(t, "git", gitSignData, "task-ok"))
	testutil.Equal(t, w.Code, http.StatusOK)
	errSign, _, _ := recordingSigner(nil, errors.New("agent signing key mismatch"))
	w = doGitSign(t, testGitSignAPI(t, errSign), "test-token", gitSignBody(t, "git", gitSignData, "task-err"))
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	w = doGitSign(t, testGitSignAPI(t, okSign), "test-token", gitSignBody(t, "file", gitSignData, "task-ns"))
	testutil.Equal(t, w.Code, http.StatusBadRequest)

	raw, err := os.ReadFile(logPath)
	testutil.NoError(t, err)
	log := string(raw)
	testutil.Contains(t, log, "[agent-git] sign task=task-ok ok")
	testutil.Contains(t, log, "[agent-git] sign task=task-err err=")
	testutil.Contains(t, log, "mismatch")
	testutil.Contains(t, log, "[agent-git] sign task=task-ns err=")
	for _, secret := range []string{
		"agent-commit-payload-marker",
		base64.StdEncoding.EncodeToString([]byte(gitSignData)),
		"SSH SIGNATURE",
		"U1NIU0lHZmFrZQ==",
	} {
		testutil.False(t, strings.Contains(log, secret))
	}
}
