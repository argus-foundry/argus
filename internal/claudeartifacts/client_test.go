package claudeartifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

const fakeBearer = "fixture-bearer-0001"

// rpcCall is one JSON-RPC request the fake endpoint received.
type rpcCall struct {
	Raw    []byte
	Ctx    context.Context
	Header http.Header
	Path   string
	Method string
	Body   struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
}

// fakeEndpoint stands in for https://api.anthropic.com/api/artifacts/mcp.
// handle answers one tool call; the default answers every tool with a fixed
// successful payload.
type fakeEndpoint struct {
	mu     sync.Mutex
	calls  []rpcCall
	handle func(w http.ResponseWriter, c rpcCall)
}

func newFakeEndpoint(t *testing.T, handle func(w http.ResponseWriter, c rpcCall)) (*fakeEndpoint, *httptest.Server) {
	t.Helper()
	f := &fakeEndpoint{handle: handle}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c rpcCall
		c.Ctx = r.Context()
		c.Header = r.Header.Clone()
		c.Path = r.URL.Path
		c.Method = r.Method
		body, _ := io.ReadAll(r.Body)
		c.Raw = body
		_ = json.Unmarshal(body, &c.Body)
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		f.handle(w, c)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeEndpoint) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeEndpoint) call(i int) rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

// toolOK writes a plain-JSON tool result whose content[0].text is payload.
func toolOK(w http.ResponseWriter, id any, payload any) {
	text, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		},
	})
}

// unavailable writes the exhausted-account refusal observed in the spike.
func unavailable(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"error":"not found: the artifact tools on this server aren't available to this session. Ask the user to check their plan."}`)
}

func testClient(srv *httptest.Server) *Client {
	return &Client{HTTP: srv.Client(), BaseURL: srv.URL, UserAgent: "argus/test"}
}

const page = "<!DOCTYPE html><html><body>hi</body></html>"

func publishHandler(t *testing.T) func(w http.ResponseWriter, c rpcCall) {
	return func(w http.ResponseWriter, c rpcCall) {
		switch c.Body.Params.Name {
		case "get_artifact":
			toolOK(w, c.Body.ID, map[string]any{"slug": c.Body.Params.Arguments["slug"], "version": "ver-3", "title": "Old"})
		case "publish_version":
			slug, _ := c.Body.Params.Arguments["slug"].(string)
			if slug == "" {
				slug = "NEWSLUG"
			}
			toolOK(w, c.Body.ID, map[string]any{
				"slug": slug, "url": "https://claude.ai/artifact/" + slug,
				"version": "ver-4", "title": "T", "read": "owner",
			})
		case "list_artifacts":
			toolOK(w, c.Body.ID, map[string]any{"artifacts": []any{}})
		default:
			t.Errorf("unexpected tool %q", c.Body.Params.Name)
		}
	}
}

func TestClientPublish_RequestShape(t *testing.T) {
	f, srv := newFakeEndpoint(t, publishHandler(t))
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "My page"})
	testutil.NoError(t, err)
	testutil.Equal(t, f.count(), 1)
	c := f.call(0)
	for _, tc := range []struct{ name, got, want string }{
		{"path", c.Path, "/api/artifacts/mcp"},
		{"method", c.Method, http.MethodPost},
		{"authorization", c.Header.Get("Authorization"), "Bearer " + fakeBearer},
		{"beta", c.Header.Get("anthropic-beta"), "oauth-2025-04-20"},
		{"accept", c.Header.Get("Accept"), "application/json, text/event-stream"},
		{"content-type", c.Header.Get("Content-Type"), "application/json"},
		{"user-agent", c.Header.Get("User-Agent"), "argus/test"},
		{"jsonrpc", c.Body.JSONRPC, "2.0"},
		{"rpc method", c.Body.Method, "tools/call"},
		{"tool", c.Body.Params.Name, "publish_version"},
		{"title", fmt.Sprint(c.Body.Params.Arguments["title"]), "My page"},
		{"content", fmt.Sprint(c.Body.Params.Arguments["content"]), page},
		{"icon", fmt.Sprint(c.Body.Params.Arguments["icon"]), "page"},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, tc.got, tc.want) })
	}
	t.Run("id present", func(t *testing.T) { testutil.True(t, c.Body.ID != nil) })
	t.Run("html sent unescaped", func(t *testing.T) {
		testutil.Contains(t, string(c.Raw), "<!DOCTYPE html><html>")
		testutil.False(t, strings.Contains(string(c.Raw), `\u003c`))
	})
	t.Run("create sends no slug", func(t *testing.T) {
		_, has := c.Body.Params.Arguments["slug"]
		testutil.False(t, has)
	})
}

func TestClientPublish_CreateReturnsLink(t *testing.T) {
	_, srv := newFakeEndpoint(t, publishHandler(t))
	res, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.URL, "https://claude.ai/artifact/NEWSLUG")
	testutil.Equal(t, res.Slug, "NEWSLUG")
	testutil.Equal(t, res.Version, "ver-4")
	testutil.Equal(t, res.Read, "owner")
}

func TestClientPublish_DefaultTitleFromFilename(t *testing.T) {
	f, srv := newFakeEndpoint(t, publishHandler(t))
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "/tmp/weekly-report.html", Content: []byte(page)})
	testutil.NoError(t, err)
	testutil.Equal(t, fmt.Sprint(f.call(0).Body.Params.Arguments["title"]), "weekly-report")
}

func TestClientPublish_Update(t *testing.T) {
	for _, tc := range []struct{ name, link, title string }{
		{"artifact link keeps title", "https://claude.ai/artifact/7RWP9zApYzozg95RP78ERg", ""},
		{"code artifact link with new title", "https://claude.ai/code/artifact/7RWP9zApYzozg95RP78ERg?x=1", "Renamed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := newFakeEndpoint(t, publishHandler(t))
			res, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{
				Name: "p.html", Content: []byte(page), Title: tc.title, UpdateURL: tc.link,
			})
			testutil.NoError(t, err)
			testutil.Equal(t, f.count(), 2)
			get := f.call(0)
			testutil.Equal(t, get.Body.Params.Name, "get_artifact")
			testutil.Equal(t, fmt.Sprint(get.Body.Params.Arguments["slug"]), "7RWP9zApYzozg95RP78ERg")
			pub := f.call(1)
			testutil.Equal(t, pub.Body.Params.Name, "publish_version")
			testutil.Equal(t, fmt.Sprint(pub.Body.Params.Arguments["slug"]), "7RWP9zApYzozg95RP78ERg")
			testutil.Equal(t, fmt.Sprint(pub.Body.Params.Arguments["base_version"]), "ver-3")
			testutil.Equal(t, fmt.Sprint(pub.Body.Params.Arguments["content"]), page)
			title, hasTitle := pub.Body.Params.Arguments["title"]
			testutil.Equal(t, hasTitle, tc.title != "")
			if tc.title != "" {
				testutil.Equal(t, fmt.Sprint(title), tc.title)
			}
			_, hasIcon := pub.Body.Params.Arguments["icon"]
			testutil.False(t, hasIcon)
			testutil.Equal(t, res.URL, "https://claude.ai/artifact/7RWP9zApYzozg95RP78ERg")
		})
	}
}

func TestClientPublish_UpdateFallsBackToGivenLink(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		if c.Body.Params.Name == "get_artifact" {
			toolOK(w, c.Body.ID, map[string]any{"version": 7})
			return
		}
		testutil.Equal(t, fmt.Sprint(c.Body.Params.Arguments["base_version"]), "7")
		toolOK(w, c.Body.ID, map[string]any{"version": 8})
	})
	res, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{
		Name: "p.html", Content: []byte(page), UpdateURL: "https://claude.ai/artifact/abc",
	})
	testutil.NoError(t, err)
	testutil.Equal(t, res.URL, "https://claude.ai/artifact/abc")
	testutil.Equal(t, res.Slug, "abc")
	testutil.Equal(t, res.Version, "8")
}

func TestClientPublish_BadUpdateLinkMakesNoRequest(t *testing.T) {
	f, srv := newFakeEndpoint(t, publishHandler(t))
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{
		Name: "p.html", Content: []byte(page), UpdateURL: "https://example.com/artifact/abc",
	})
	testutil.ErrorIs(t, err, ErrInvalidInput)
	testutil.Equal(t, f.count(), 0)
}

func TestClient_ErrorMapping(t *testing.T) {
	long := strings.Repeat("x", 1000)
	for _, tc := range []struct {
		name     string
		handle   func(w http.ResponseWriter, c rpcCall)
		wantIs   error
		wantText []string
	}{
		{
			name:   "unavailable 404",
			handle: func(w http.ResponseWriter, _ rpcCall) { unavailable(w) },
			wantIs: ErrAccountUnavailable,
		},
		{
			name: "unavailable 404 with curly apostrophe",
			handle: func(w http.ResponseWriter, _ rpcCall) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, "not found: the artifact tools aren’t available to this session")
			},
			wantIs: ErrAccountUnavailable,
		},
		{
			name: "unavailable tool error",
			handle: func(w http.ResponseWriter, c rpcCall) {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "result": map[string]any{
					"isError": true, "content": []map[string]any{{"type": "text", "text": "these tools aren't available to this session"}},
				}})
			},
			wantIs: ErrAccountUnavailable,
		},
		{
			name: "401",
			handle: func(w http.ResponseWriter, _ rpcCall) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":"invalid token"}`)
			},
			wantIs: ErrUnauthorized,
		},
		{
			name: "plain 404 is not unavailable",
			handle: func(w http.ResponseWriter, _ rpcCall) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, "no such route")
			},
			wantText: []string{"HTTP 404", "no such route"},
		},
		{
			name: "500 with body excerpt capped",
			handle: func(w http.ResponseWriter, _ rpcCall) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "boom"+long)
			},
			wantText: []string{"HTTP 500", "boom" + strings.Repeat("x", 296)},
		},
		{
			name: "json-rpc error object",
			handle: func(w http.ResponseWriter, c rpcCall) {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "error": map[string]any{"code": -32602, "message": "bad params"}})
			},
			wantText: []string{"bad params", "-32602"},
		},
		{
			name: "tool error result",
			handle: func(w http.ResponseWriter, c rpcCall) {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "result": map[string]any{
					"isError": true, "content": []map[string]any{{"type": "text", "text": "content too large"}},
				}})
			},
			wantText: []string{"content too large"},
		},
		{
			name:     "unparseable body",
			handle:   func(w http.ResponseWriter, _ rpcCall) { _, _ = io.WriteString(w, "<html>gateway</html>") },
			wantText: []string{"decode", "gateway"},
		},
		{
			name: "server echoes the token",
			handle: func(w http.ResponseWriter, c rpcCall) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, "upstream rejected "+c.Header.Get("Authorization"))
			},
			wantText: []string{"HTTP 502", "[redacted]"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := newFakeEndpoint(t, tc.handle)
			_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
			testutil.Error(t, err)
			if tc.wantIs != nil {
				testutil.ErrorIs(t, err, tc.wantIs)
			} else {
				testutil.False(t, errors.Is(err, ErrAccountUnavailable))
				testutil.False(t, errors.Is(err, ErrUnauthorized))
			}
			for _, want := range tc.wantText {
				testutil.Contains(t, err.Error(), want)
			}
			testutil.False(t, strings.Contains(err.Error(), fakeBearer))
			testutil.False(t, strings.Contains(err.Error(), strings.Repeat("x", 301)))
		})
	}
}

func TestClient_SSEResponse(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		w.Header().Set("Content-Type", "text/event-stream")
		inner, _ := json.Marshal(map[string]any{"slug": "S", "url": "https://claude.ai/artifact/S", "version": "v1"})
		final, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(inner)}},
		}})
		_, _ = fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\nevent: message\ndata: %s\n\n", final)
	})
	res, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.URL, "https://claude.ai/artifact/S")
}

func TestClient_StructuredContentPreferred(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "result": map[string]any{
			"structuredContent": map[string]any{"slug": "FROMSTRUCT", "url": "https://claude.ai/artifact/FROMSTRUCT"},
			"content":           []map[string]any{{"type": "text", "text": `{"slug":"FROMTEXT"}`}},
		}})
	})
	res, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.Slug, "FROMSTRUCT")
}

func TestClient_MissingResultContent(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": c.Body.ID, "result": map[string]any{"content": []any{}}})
	})
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "no content")
}

func TestClient_NetworkErrorRedacted(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", UserAgent: "argus/test"}
	_, err := c.Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.Error(t, err)
	testutil.False(t, strings.Contains(err.Error(), fakeBearer))
}

func TestClientProbe(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		f, srv := newFakeEndpoint(t, publishHandler(t))
		testutil.NoError(t, testClient(srv).Probe(context.Background(), fakeBearer))
		c := f.call(0)
		testutil.Equal(t, c.Body.Params.Name, "list_artifacts")
		testutil.Equal(t, fmt.Sprint(c.Body.Params.Arguments["limit"]), "1")
	})
	t.Run("unavailable", func(t *testing.T) {
		_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, _ rpcCall) { unavailable(w) })
		testutil.ErrorIs(t, testClient(srv).Probe(context.Background(), fakeBearer), ErrAccountUnavailable)
	})
}

func TestClient_Defaults(t *testing.T) {
	c := &Client{}
	testutil.Equal(t, c.endpoint(), "https://api.anthropic.com/api/artifacts/mcp")
	testutil.Equal(t, c.userAgent(), "argus")
	testutil.NotNil(t, c.httpClient())
}

func TestParseArtifactSlug(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"https://claude.ai/artifact/7RWP9zApYzozg95RP78ERg", "7RWP9zApYzozg95RP78ERg", true},
		{"https://claude.ai/code/artifact/abc_DEF-1/", "abc_DEF-1", true},
		{"https://www.claude.ai/artifact/wwwSlug", "wwwSlug", true},
		{"https://claude.ai/artifact/abc#frag", "abc", true},
		{"  https://claude.ai/artifact/abc  ", "abc", true},
		{"abc123", "abc123", true},
		{"https://example.com/artifact/abc", "", false},
		{"https://claude.ai/chat/abc", "", false},
		{"https://claude.ai/artifact/", "", false},
		{"https://claude.ai/artifact/a/b", "", false},
		{"not a slug!", "", false},
		{"", "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseArtifactSlug(tc.in)
			if !tc.ok {
				testutil.ErrorIs(t, err, ErrInvalidInput)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, got, tc.want)
		})
	}
}

func TestRedactedError(t *testing.T) {
	inner := fmt.Errorf("dial with %s: %w", fakeBearer, ErrUnauthorized)
	err := error(redactedError{err: inner, token: fakeBearer})
	testutil.Equal(t, err.Error(), "dial with [redacted]: "+ErrUnauthorized.Error())
	testutil.ErrorIs(t, err, ErrUnauthorized)
}

func TestFlexString(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{`"v-1"`, "v-1", true},
		{`42`, "42", true},
		{`{}`, "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var f flexString
			err := json.Unmarshal([]byte(tc.in), &f)
			if !tc.ok {
				testutil.Error(t, err)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, string(f), tc.want)
		})
	}
}

func TestClientPublish_UpdateWithoutVersion(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		toolOK(w, c.Body.ID, map[string]any{"slug": "abc"})
	})
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{
		Name: "p.html", Content: []byte(page), UpdateURL: "https://claude.ai/artifact/abc",
	})
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "no version")
}

func TestClientPublish_NoSlugOrURL(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, c rpcCall) {
		toolOK(w, c.Body.ID, map[string]any{"version": "v1"})
	})
	_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "no slug or url")
}
