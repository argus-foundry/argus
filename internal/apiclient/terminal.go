package apiclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/argus-foundry/argus/internal/app/agentview"
)

// inputOriginHeader carries the WriteInput origin over REST. Only sent for
// agentview.OriginSystem; agentview.OriginUser is the zero value and the
// header is omitted for it, so an older server (which does not look at this
// header at all) and a newer server presented with no header both apply the
// same OriginUser default — see handleWriteInput's read side.
const inputOriginHeader = "X-Input-Origin"

// inputOriginSystemValue is the header value signaling agentview.OriginSystem.
const inputOriginSystemValue = "system"

// OutputResult holds recent output and its byte cursor. A nonzero PID scopes the
// cursor to the process that produced it. Source is "ring", "log", or "live".
type OutputResult struct {
	Data        []byte
	OutputTotal uint64
	PID         int
	Source      string
}

// GetOutput fetches the most recent terminal output for a task. tailBytes
// caps the response body; pass 0 for the server default (32 KiB). clean
// strips ANSI control sequences when true.
func (c *Client) GetOutput(ctx context.Context, id string, tailBytes int, clean bool) (*OutputResult, error) {
	q := ""
	if tailBytes > 0 || clean {
		q = "?"
		if tailBytes > 0 {
			q += "bytes=" + strconv.Itoa(tailBytes)
		}
		if clean {
			if q != "?" {
				q += "&"
			}
			q += "clean=1"
		}
	}
	resp, err := c.do(ctx, "GET", "/api/tasks/"+id+"/output"+q, nil, "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("apiclient: read output body: %w", err)
	}
	total := uint64(0)
	if v := resp.Header.Get("X-Output-Total"); v != "" {
		if n, perr := strconv.ParseUint(v, 10, 64); perr == nil {
			total = n
		}
	}
	pid, _ := strconv.Atoi(resp.Header.Get("X-Argus-Session-Pid"))
	return &OutputResult{Data: data, OutputTotal: total, PID: pid, Source: resp.Header.Get("X-Source")}, nil
}

// WriteInput sends bytes to the agent's PTY. Capped at 64 KiB by the server.
// origin threads through as the X-Input-Origin header (see inputOriginHeader);
// OriginUser sends no header at all (the safe, pre-existing wire shape).
func (c *Client) WriteInput(ctx context.Context, id string, data []byte, origin agentview.InputOrigin) error {
	var headers map[string]string
	if origin == agentview.OriginSystem {
		headers = map[string]string{inputOriginHeader: inputOriginSystemValue}
	}
	resp, err := c.do(ctx, "POST", "/api/tasks/"+id+"/input", bytesReader(data), "application/octet-stream", headers)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PTYSize is the cols/rows the active session is sized to.
type PTYSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// GetSize fetches the active PTY size for a task.
func (c *Client) GetSize(ctx context.Context, id string) (*PTYSize, error) {
	var resp PTYSize
	if err := c.doJSON(ctx, "GET", "/api/tasks/"+id+"/size", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ResizeResp mirrors the resize handler's response envelope.
type ResizeResp struct {
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	Rerendered bool   `json:"rerendered"`
	Deferred   string `json:"deferred"`
}

// Resize sends a SIGWINCH-equivalent resize to the agent's PTY.
func (c *Client) Resize(ctx context.Context, id string, rows, cols uint16) (*ResizeResp, error) {
	return c.resize(ctx, id, rows, cols, RerenderResizeOptions{})
}

// RerenderResizeOptions describe the remote pane's current process and history.
type RerenderResizeOptions struct {
	ExpectedPID   int    `json:"expected_pid,omitempty"`
	CommittedCols uint16 `json:"committed_cols,omitempty"`
	RerenderNow   bool   `json:"rerender_now,omitempty"`
	ScrolledUp    bool   `json:"scrolled_up,omitempty"`
	ComposerDirty bool   `json:"composer_dirty,omitempty"`
}

// ResizeForRerender asks the session owner to evaluate the remote pane's width.
func (c *Client) ResizeForRerender(ctx context.Context, id string, rows, cols uint16, options RerenderResizeOptions) (*ResizeResp, error) {
	return c.resize(ctx, id, rows, cols, options)
}

func (c *Client) resize(ctx context.Context, id string, rows, cols uint16, options RerenderResizeOptions) (*ResizeResp, error) {
	req := struct {
		Rows uint16 `json:"rows"`
		Cols uint16 `json:"cols"`
		RerenderResizeOptions
	}{rows, cols, options}
	var resp ResizeResp
	if err := c.doJSON(ctx, "POST", "/api/tasks/"+id+"/resize", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StreamOutput opens a long-lived SSE connection to the task's PTY output.
// The returned http.Response.Body is the raw event stream — caller is
// responsible for parsing the `data: <base64>\n\n` framing and closing the
// body. since is the cursor previously advertised by GetOutput.
//
// This bypasses the *http.Client.Timeout because SSE is long-lived; the
// caller controls cancellation via ctx.
func (c *Client) StreamOutput(ctx context.Context, id string, since uint64) (*http.Response, error) {
	return c.StreamOutputForPID(ctx, id, since, 0)
}

// StreamOutputForPID uses the cursor only if the expected PTY still owns it.
func (c *Client) StreamOutputForPID(ctx context.Context, id string, since uint64, expectedPID int) (*http.Response, error) {
	path := "/api/tasks/" + id + "/stream"
	query := url.Values{}
	if since > 0 {
		query.Set("since", strconv.FormatUint(since, 10))
	}
	if expectedPID > 0 {
		query.Set("expected_pid", strconv.Itoa(expectedPID))
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	// Bypass c.do because we want to return the live body without consuming
	// it. Use a Request directly so we can disable any client-side timeout.
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("apiclient: build stream req: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "text/event-stream")
	// Use a stream-dedicated client without Timeout so chunked transfer can
	// sit idle indefinitely (server emits a 30s keepalive `:ping` to keep
	// proxies honest).
	hc := *c.hc
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiclient: stream %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := readErrorMessage(resp.Body)
		resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Method: "GET", Path: path, Message: msg}
	}
	return resp, nil
}
