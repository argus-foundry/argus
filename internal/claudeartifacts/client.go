// Package claudeartifacts publishes single-page HTML artifacts to claude.ai
// through the artifacts MCP endpoint Claude Code's built-in artifact tool
// uses, authenticated with a per-account `claude setup-token` token.
//
// The endpoint is undocumented. What is known (spike, 2026-11-09):
//   - It is stateless JSON-RPC 2.0 over POST. `initialize` answers 404, so the
//     client sends `tools/call` directly.
//   - Responses are plain JSON or SSE; for SSE the last `data:` line holds the
//     reply.
//   - An account at 0% budget answers every tool call with HTTP 404 and a body
//     saying the tools "aren't available to this session". Its cut-down tool
//     list still names publish_version, so only a real call tells.
//
// Every failure is surfaced loudly with the status and a body excerpt; there
// is no silent fallback. The token appears only in the Authorization header
// and is scrubbed from every error this package returns.
package claudeartifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// DefaultBaseURL is the host the artifacts endpoint lives on.
	DefaultBaseURL = "https://api.anthropic.com"
	// EndpointPath is the artifacts MCP endpoint's path.
	EndpointPath = "/api/artifacts/mcp"
	// betaHeader is the anthropic-beta value OAuth tokens need.
	betaHeader = "oauth-2025-04-20"
	// defaultIcon is the tab icon new artifacts get.
	defaultIcon = "page"
	// bodyExcerptBytes caps how much of an error body an error carries.
	bodyExcerptBytes = 300
	// maxResponseBytes bounds how much of a response is read.
	maxResponseBytes = 4 << 20
	// defaultHTTPTimeout bounds one request when the caller supplies no client.
	defaultHTTPTimeout = 60 * time.Second
	// unavailableMarker is the refusal text of an account whose artifact tools
	// are switched off (observed for an account at 0% budget). Matched without
	// the apostrophe so a typographic quote still matches.
	unavailableMarker = "available to this session"
)

var (
	// ErrAccountUnavailable: the account's artifact tools are switched off
	// (usage exhausted or otherwise blocked).
	ErrAccountUnavailable = errors.New("artifact tools are not available to this account right now")
	// ErrUnauthorized: the endpoint rejected the token (HTTP 401).
	ErrUnauthorized = errors.New("artifacts endpoint rejected the token")
	// ErrInvalidInput: the page or link was rejected locally, before any
	// request was made.
	ErrInvalidInput = errors.New("invalid artifact input")
)

// Client is a stateless client for the artifacts endpoint. The zero value
// talks to DefaultBaseURL with a 60s timeout.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string

	nextID atomic.Int64
}

// PublishInput is one page to publish.
type PublishInput struct {
	// Name is the page's file name; it must end in .html or .htm and, when
	// Title is empty on a create, supplies the title.
	Name    string
	Content []byte
	// Title is the artifact title. On an update an empty title keeps the
	// current one.
	Title string
	// UpdateURL, when set, is the claude.ai link of an existing artifact to
	// publish a new version of.
	UpdateURL string
}

// Result describes a published artifact version.
type Result struct {
	Slug    string `json:"slug"`
	URL     string `json:"url"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
	Read    string `json:"read,omitempty"`
}

func (c *Client) endpoint() string {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	return strings.TrimRight(base, "/") + EndpointPath
}

func (c *Client) userAgent() string {
	if c.UserAgent == "" {
		return "argus"
	}
	return c.UserAgent
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: defaultHTTPTimeout}
	}
	return c.HTTP
}

// Publish creates a new artifact, or a new version of in.UpdateURL's artifact
// based on its current version. Input is validated before any request.
func (c *Client) Publish(ctx context.Context, token string, in PublishInput) (Result, error) {
	if err := ValidateHTML(in.Name, in.Content); err != nil {
		return Result{}, err
	}
	args := map[string]any{"content": string(in.Content)}
	var slug string
	if in.UpdateURL != "" {
		var err error
		if slug, err = ParseArtifactSlug(in.UpdateURL); err != nil {
			return Result{}, err
		}
		var current struct {
			Version flexString `json:"version"`
		}
		if err := c.callTool(ctx, token, "get_artifact", map[string]any{"slug": slug}, &current); err != nil {
			return Result{}, fmt.Errorf("look up artifact %s: %w", slug, err)
		}
		if current.Version == "" {
			return Result{}, fmt.Errorf("look up artifact %s: response carried no version", slug)
		}
		args["slug"] = slug
		args["base_version"] = string(current.Version)
		if in.Title != "" {
			args["title"] = in.Title
		}
	} else {
		title := in.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(in.Name), filepath.Ext(in.Name))
		}
		args["title"] = title
		args["icon"] = defaultIcon
	}

	var out struct {
		Slug    string     `json:"slug"`
		URL     string     `json:"url"`
		Version flexString `json:"version"`
		Title   string     `json:"title"`
		Read    string     `json:"read"`
	}
	if err := c.callTool(ctx, token, "publish_version", args, &out); err != nil {
		return Result{}, fmt.Errorf("publish: %w", err)
	}
	res := Result{Slug: out.Slug, URL: out.URL, Version: string(out.Version), Title: out.Title, Read: out.Read}
	if res.Slug == "" {
		res.Slug = slug
	}
	if res.URL == "" && res.Slug != "" {
		res.URL = "https://claude.ai/artifact/" + res.Slug
	}
	if res.URL == "" {
		return Result{}, errors.New("publish: response carried no slug or url")
	}
	return res, nil
}

// Probe reports whether the account behind token can use the artifact tools,
// with a real list_artifacts call (the tool list is not a reliable signal).
func (c *Client) Probe(ctx context.Context, token string) error {
	return c.callTool(ctx, token, "list_artifacts", map[string]any{"limit": 1}, nil)
}

// rpcResponse is a JSON-RPC reply to tools/call.
type rpcResponse struct {
	Result *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callTool sends one tools/call and decodes the tool's JSON payload into out
// (nil discards it). Every returned error has the token scrubbed.
func (c *Client) callTool(ctx context.Context, token, name string, args, out any) error {
	err := c.doCallTool(ctx, token, name, args, out)
	if err != nil && token != "" && strings.Contains(err.Error(), token) {
		return redactedError{err: err, token: token}
	}
	return err
}

func (c *Client) doCallTool(ctx context.Context, token, name string, args, out any) error {
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		return fmt.Errorf("encode %s request: %w", name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build %s request: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close() //nolint:errcheck // body fully read below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%s: read response: %w", name, err)
	}
	if token != "" {
		// Scrub before any excerpt is cut, so a truncation can't leave a
		// partial token behind.
		raw = bytes.ReplaceAll(raw, []byte(token), []byte("[redacted]"))
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%s: %w (HTTP 401)", name, ErrUnauthorized)
	case resp.StatusCode == http.StatusNotFound && strings.Contains(string(raw), unavailableMarker):
		return fmt.Errorf("%s: %w", name, ErrAccountUnavailable)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("%s: artifacts endpoint returned HTTP %d: %s", name, resp.StatusCode, excerpt(raw))
	}

	payload := lastSSEData(raw)
	var rpc rpcResponse
	if err := json.Unmarshal(payload, &rpc); err != nil {
		return fmt.Errorf("%s: decode response: %w: %s", name, err, excerpt(payload))
	}
	if rpc.Error != nil {
		return fmt.Errorf("%s: JSON-RPC error %d: %s", name, rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result == nil {
		return fmt.Errorf("%s: response has no result: %s", name, excerpt(payload))
	}
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	if rpc.Result.IsError {
		if strings.Contains(text, unavailableMarker) {
			return fmt.Errorf("%s: %w", name, ErrAccountUnavailable)
		}
		return fmt.Errorf("%s: tool error: %s", name, excerpt([]byte(text)))
	}
	if out == nil {
		return nil
	}
	body := []byte(text)
	if len(rpc.Result.StructuredContent) > 0 && string(rpc.Result.StructuredContent) != "null" {
		body = rpc.Result.StructuredContent
	} else if len(rpc.Result.Content) == 0 {
		return fmt.Errorf("%s: result has no content", name)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decode result: %w: %s", name, err, excerpt(body))
	}
	return nil
}

// lastSSEData returns the last `data:` payload of an SSE body, or raw itself
// when the body is plain JSON.
func lastSSEData(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return raw
	}
	var last []byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			last = bytes.TrimSpace(rest)
		}
	}
	if last == nil {
		return raw
	}
	return last
}

// excerpt returns at most bodyExcerptBytes of b, trimmed.
func excerpt(b []byte) string {
	if len(b) > bodyExcerptBytes {
		b = b[:bodyExcerptBytes]
	}
	return strings.TrimSpace(string(b))
}

// redactedError replaces the token in an error's text, keeping errors.Is.
type redactedError struct {
	err   error
	token string
}

func (e redactedError) Error() string {
	return strings.ReplaceAll(e.err.Error(), e.token, "[redacted]")
}

func (e redactedError) Unwrap() error { return e.err }

// flexString decodes a JSON string or number as a string: version ids are
// opaque, so either form is passed back unchanged.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("version is neither a string nor a number: %s", b)
	}
	*f = flexString(n.String())
	return nil
}
