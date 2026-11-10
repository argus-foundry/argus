package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/claudeartifacts"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

const (
	apiAcctOK      = "tok-ok-a1"
	apiAcctGone    = "tok-gone-a2"
	apiAcctExpired = "tok-expired-a3"
	apiPublishPage = "<!DOCTYPE html><html><body>report</body></html>"
)

// fakeArtifactsUpstream stands in for the claude.ai artifacts endpoint.
func fakeArtifactsUpstream(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		var body struct {
			ID any `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.Header.Get("Authorization") {
		case "Bearer " + apiAcctGone:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "the artifact tools on this server aren't available to this session")
			return
		case "Bearer " + apiAcctExpired:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		text, _ := json.Marshal(map[string]any{"slug": "S1", "url": "https://claude.ai/artifact/S1", "version": "v1", "title": "T"})
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func testPublishAPI(t *testing.T) (http.Handler, *atomic.Int32) {
	t.Helper()
	srv, _ := testServer(t)
	upstream, n := fakeArtifactsUpstream(t)
	cfg := config.Config{ArtifactTokens: map[string]string{
		"personal": "env://OK",
		"default":  "env://GONE",
		"expired":  "env://EXPIRED",
	}}
	tokens := map[string]string{"env://OK": apiAcctOK, "env://GONE": apiAcctGone, "env://EXPIRED": apiAcctExpired}
	srv.SetArtifactPublisher(&claudeartifacts.Publisher{
		Client:  &claudeartifacts.Client{HTTP: upstream.Client(), BaseURL: upstream.URL},
		Config:  func() config.Config { return cfg },
		Resolve: func(_ config.SecretsConfig, s string) (string, bool) { v, ok := tokens[s]; return v, ok },
	})
	handler := authMiddleware(srv.token, srv.db, srv.push, srv.routes(),
		"/", "/share", "/vendor/", "/manifest.webmanifest", "/sw.js")
	return handler, n
}

// publishForm builds a multipart publish request body.
func publishForm(t *testing.T, fields map[string]string, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		testutil.NoError(t, mw.WriteField(k, v))
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		testutil.NoError(t, err)
		_, _ = io.WriteString(fw, content)
	}
	testutil.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func doPublish(t *testing.T, h http.Handler, token string, fields map[string]string, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, ctype := publishForm(t, fields, filename, content)
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/publish", body)
	req.Header.Set("Content-Type", ctype)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func assertNoAPIToken(t *testing.T, body string) {
	t.Helper()
	for _, tok := range []string{apiAcctOK, apiAcctGone, apiAcctExpired} {
		testutil.False(t, strings.Contains(body, tok))
	}
}

func TestPublishArtifact_Success(t *testing.T) {
	h, n := testPublishAPI(t)
	w := doPublish(t, h, "test-token", map[string]string{"account": "personal", "title": "Report"}, "report.html", apiPublishPage)
	testutil.Equal(t, w.Code, http.StatusOK)
	var got publishArtifactResponse
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Status, "published")
	testutil.Equal(t, got.Account, "personal")
	testutil.Equal(t, got.URL, "https://claude.ai/artifact/S1")
	testutil.Equal(t, n.Load(), int32(1))
	assertNoAPIToken(t, w.Body.String())
}

func TestPublishArtifact_Unavailable(t *testing.T) {
	h, _ := testPublishAPI(t)
	w := doPublish(t, h, "test-token", map[string]string{"account": "default"}, "report.html", apiPublishPage)
	testutil.Equal(t, w.Code, http.StatusOK)
	var got publishArtifactResponse
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Status, "unavailable")
	testutil.Equal(t, got.Account, "default")
	// "expired" fails its probe, so only "personal" can publish.
	testutil.DeepEqual(t, got.AvailableAccounts, []string{"personal"})
	assertNoAPIToken(t, w.Body.String())
}

func TestPublishArtifact_Errors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fields   map[string]string
		file     string
		content  string
		wantCode int
		want     string
		wantReqs int32
	}{
		{"missing account", map[string]string{}, "r.html", apiPublishPage, http.StatusBadRequest, "account is required", 0},
		{"missing file", map[string]string{"account": "personal"}, "", "", http.StatusBadRequest, "file is required", 0},
		{"no token configured", map[string]string{"account": "work"}, "r.html", apiPublishPage, http.StatusBadRequest, "[artifact_tokens]", 0},
		{"not html", map[string]string{"account": "personal"}, "r.txt", apiPublishPage, http.StatusBadRequest, ".html or .htm", 0},
		{"bad update link", map[string]string{"account": "personal", "update": "https://example.com/x"}, "r.html", apiPublishPage, http.StatusBadRequest, "claude.ai", 0},
		{"expired token", map[string]string{"account": "expired"}, "r.html", apiPublishPage, http.StatusBadGateway, "claude setup-token", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, n := testPublishAPI(t)
			w := doPublish(t, h, "test-token", tc.fields, tc.file, tc.content)
			testutil.Equal(t, w.Code, tc.wantCode)
			testutil.Contains(t, w.Body.String(), tc.want)
			testutil.Equal(t, n.Load(), tc.wantReqs)
			assertNoAPIToken(t, w.Body.String())
		})
	}
}

func TestPublishArtifact_BadBody(t *testing.T) {
	h, _ := testPublishAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/publish", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	testutil.Equal(t, w.Code, http.StatusBadRequest)
	testutil.Contains(t, w.Body.String(), "multipart")
}

func TestPublishArtifact_RequiresAuth(t *testing.T) {
	h, n := testPublishAPI(t)
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "nope", http.StatusUnauthorized},
		{"master token", "test-token", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doPublish(t, h, tc.token, map[string]string{"account": "personal"}, "r.html", apiPublishPage)
			testutil.Equal(t, w.Code, tc.want)
		})
	}
	testutil.Equal(t, n.Load(), int32(1))
}

func TestPublishArtifact_NotConfigured(t *testing.T) {
	srv, _ := testServer(t)
	body, ctype := publishForm(t, map[string]string{"account": "personal"}, "r.html", apiPublishPage)
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/publish", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
}

func TestPublishArtifact_BodyTooLarge(t *testing.T) {
	h, n := testPublishAPI(t)
	big := apiPublishPage + strings.Repeat("x", maxPublishBody)
	w := doPublish(t, h, "test-token", map[string]string{"account": "personal"}, "r.html", big)
	testutil.Equal(t, w.Code, http.StatusRequestEntityTooLarge)
	testutil.Contains(t, w.Body.String(), "16 MiB")
	testutil.Equal(t, n.Load(), int32(0))
}
