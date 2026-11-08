// Package codexctx reads the current context-window fill of a Codex session
// from the rollout file Codex itself records. It is read-only, never starts a
// CLI or a model turn, and bounds every file read.
package codexctx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// TailBytes bounds how much of a rollout's end Latest parses. A token_count
// event follows every model call, so a window this size always holds one for a
// session that has made a call recently.
const TailBytes = 1 << 20

// Reading is one measurement of a session's context.
type Reading struct {
	MeasuredAt time.Time
	// UsedTokens matches Codex's last_token_usage.total_tokens, including compaction estimates.
	UsedTokens int64
	// WindowTokens is 0 when the rollout recorded no window.
	WindowTokens int64
	Model        string
}

type usage struct {
	TotalTokens *int64 `json:"total_tokens"`
}

type line struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  *struct {
			Last   *usage `json:"last_token_usage"`
			Window int64  `json:"model_context_window"`
		} `json:"info"`
	} `json:"payload"`
}

var (
	tokenCountMarker  = []byte(`"token_count"`)
	turnContextMarker = []byte(`"turn_context"`)
)

// Latest returns the newest measurement in the last TailBytes of the rollout
// of the given size. ok is false when that tail holds no usable token_count
// event (a fresh session, or one idle for longer than the window covers).
// Unparseable lines are skipped, never an error.
func Latest(r io.ReaderAt, size int64) (Reading, bool, error) {
	start := max(size-TailBytes, 0)
	buf := make([]byte, size-start)
	n, err := r.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return Reading{}, false, err
	}
	buf = buf[:n]
	if start > 0 {
		// The window opens mid-line: drop the partial first line.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			return Reading{}, false, nil
		}
	}
	// A torn final line is Codex mid-write: ignore it.
	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
		buf = buf[:i+1]
	} else {
		return Reading{}, false, nil
	}
	var out Reading
	var model string
	var found bool
	for _, raw := range bytes.Split(buf, []byte{'\n'}) {
		isTurn := bytes.Contains(raw, turnContextMarker)
		if !isTurn && !bytes.Contains(raw, tokenCountMarker) {
			continue
		}
		var l line
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		switch {
		case l.Type == "turn_context" && l.Payload.Model != "":
			model = l.Payload.Model
		case l.Type == "event_msg" && l.Payload.Type == "token_count":
			info := l.Payload.Info
			if info == nil || info.Last == nil || info.Last.TotalTokens == nil || *info.Last.TotalTokens < 0 {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
			if err != nil {
				continue
			}
			u := info.Last
			out = Reading{
				MeasuredAt:   at,
				UsedTokens:   *u.TotalTokens,
				WindowTokens: max(info.Window, 0),
				Model:        model,
			}
			found = true
		}
	}
	return out, found, nil
}
