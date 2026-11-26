package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// The signing route is loopback + master token only: a per-device token
// (phone PWA, remote TUI) or a Tailscale peer must never obtain a signature.
func TestGitSign_RefusesNonLocalCallers(t *testing.T) {
	const deviceToken = "device-token-plain"
	for _, tc := range []struct {
		name       string
		remoteAddr string
		token      string
		header     [2]string
	}{
		{"device token from loopback", "127.0.0.1:50000", deviceToken, [2]string{}},
		{"master token from a tailscale peer", "100.94.76.10:50000", "test-token", [2]string{}},
		{"master token from ipv6 non-loopback", "[fd7a:115c:a1e0::1]:50000", "test-token", [2]string{}},
		{"master token proxied by tailscale serve", "127.0.0.1:50000", "test-token", [2]string{"X-Forwarded-For", "100.94.76.10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sign, _, n := recordingSigner([]byte(gitSignSignature), nil)
			srv, d := testServer(t)
			srv.SetGitSigner(sign)
			_, err := d.AddAPIToken("phone", hashToken(deviceToken), "lain")
			testutil.NoError(t, err)
			h := authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/")

			req := httptest.NewRequest(http.MethodPost, "/api/git/sign", strings.NewReader(gitSignBody(t, "git", gitSignData, "t")))
			req.RemoteAddr = tc.remoteAddr
			req.Header.Set("Authorization", "Bearer "+tc.token)
			if tc.header[0] != "" {
				req.Header.Set(tc.header[0], tc.header[1])
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			testutil.Equal(t, w.Code, http.StatusForbidden)
			testutil.Equal(t, n.Load(), int32(0))
			testutil.False(t, strings.Contains(w.Body.String(), "SSH SIGNATURE"))
		})
	}
}
