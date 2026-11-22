package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// lockedBuf is a goroutine-safe log sink for the swapped slog default.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The daemon never calls uxlog.Init; runDaemon points slog at daemon.log, so
// the sign outcome must also reach slog.
func TestGitSign_LogsToSlog(t *testing.T) {
	sink := &lockedBuf{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	okSign, _, _ := recordingSigner([]byte(gitSignSignature), nil)
	w := doGitSign(t, testGitSignAPI(t, okSign), "test-token", gitSignBody(t, "git", gitSignData, "task-slog"))
	testutil.Equal(t, w.Code, http.StatusOK)

	got := sink.String()
	testutil.Contains(t, got, "[agent-git] sign task=task-slog ok")
	testutil.False(t, strings.Contains(got, "agent-commit-payload-marker"))
	testutil.False(t, strings.Contains(got, "SSH SIGNATURE"))
}

func TestGitSign_EmptySignatureIs503(t *testing.T) {
	sign, _, _ := recordingSigner(nil, nil)
	w := doGitSign(t, testGitSignAPI(t, sign), "test-token", gitSignBody(t, "git", gitSignData, "task-1"))
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	testutil.Contains(t, errorField(t, w), "empty signature")
}

func TestLogTaskID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"task-1", "task-1"},
		{"", "-"},
		{"  \n", "-"},
		{"a\nforged [agent-git] sign task=x ok", "a forged [agent-git] sign task=x ok"},
		{"a\x00b", "a b"},
		{strings.Repeat("x", 200), strings.Repeat("x", 128)},
	} {
		t.Run(tc.in, func(t *testing.T) {
			testutil.Equal(t, logTaskID(tc.in), tc.want)
		})
	}
}
