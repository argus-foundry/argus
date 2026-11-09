package codexctx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// UsageTailBytes bounds how much of a rollout's end Usage parses. A turn that
// began before the window is dropped rather than reported partially.
const UsageTailBytes = 16 << 20

// Counters are token counts; nil means the rollout did not report that field,
// which is distinct from a measured zero. InputTokens includes CachedInputTokens.
type Counters struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
}

// Turn is the token spend of one model turn.
type Turn struct {
	ID         string
	StartedAt  time.Time
	MeasuredAt time.Time
	Counters
}

// SessionUsage is the token accounting of one rollout.
type SessionUsage struct {
	MeasuredAt time.Time
	Totals     Counters
	Turns      []Turn
}

type usageCounts struct {
	Input  *int64 `json:"input_tokens"`
	Cached *int64 `json:"cached_input_tokens"`
	Output *int64 `json:"output_tokens"`
}

type usageLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type   string `json:"type"`
		TurnID string `json:"turn_id"`
		Info   *struct {
			Total *usageCounts `json:"total_token_usage"`
			Last  *usageCounts `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

var taskStartedMarker = []byte(`"task_started"`)

func addPtr(dst **int64, v *int64) {
	if v == nil || *v < 0 {
		return
	}
	if *dst == nil {
		z := int64(0)
		*dst = &z
	}
	**dst += *v
}

func clonePtr(v *int64) *int64 {
	if v == nil || *v < 0 {
		return nil
	}
	c := *v
	return &c
}

func equalCounts(a, b *usageCounts) bool {
	eq := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a != nil && b != nil && eq(a.Input, b.Input) && eq(a.Cached, b.Cached) && eq(a.Output, b.Output)
}

// Usage derives session totals and per-turn spend from the last UsageTailBytes
// of the rollout. Totals come from the cumulative total_token_usage, never a
// sum of per-call usage. A turn sums its calls' last_token_usage; a repeated
// identical token_count (Codex re-emits it) is counted once. Unparseable lines
// are skipped. ok is false when the window holds no usable token_count.
func Usage(r io.ReaderAt, size int64) (SessionUsage, bool, error) {
	start := max(size-UsageTailBytes, 0)
	buf := make([]byte, size-start)
	n, err := r.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return SessionUsage{}, false, err
	}
	buf = buf[:n]
	if start > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return SessionUsage{}, false, nil
		}
		buf = buf[i+1:]
	}
	i := bytes.LastIndexByte(buf, '\n')
	if i < 0 {
		return SessionUsage{}, false, nil
	}
	buf = buf[:i+1]

	var out SessionUsage
	var found bool
	var cur *Turn
	var prev *usageCounts
	flush := func() {
		if cur != nil {
			out.Turns = append(out.Turns, *cur)
			cur = nil
		}
	}
	for _, raw := range bytes.Split(buf, []byte{'\n'}) {
		isStart := bytes.Contains(raw, taskStartedMarker)
		if !isStart && !bytes.Contains(raw, tokenCountMarker) {
			continue
		}
		var l usageLine
		if json.Unmarshal(raw, &l) != nil || l.Type != "event_msg" {
			continue
		}
		at, perr := time.Parse(time.RFC3339Nano, l.Timestamp)
		if perr != nil {
			continue
		}
		switch l.Payload.Type {
		case "task_started":
			if l.Payload.TurnID == "" {
				continue
			}
			flush()
			cur = &Turn{ID: l.Payload.TurnID, StartedAt: at}
		case "token_count":
			info := l.Payload.Info
			if info == nil || info.Total == nil {
				continue
			}
			found = true
			out.MeasuredAt = at
			out.Totals = Counters{InputTokens: clonePtr(info.Total.Input), CachedInputTokens: clonePtr(info.Total.Cached), OutputTokens: clonePtr(info.Total.Output)}
			dup := equalCounts(prev, info.Total)
			prev = info.Total
			if cur == nil || dup || info.Last == nil {
				continue
			}
			addPtr(&cur.InputTokens, info.Last.Input)
			addPtr(&cur.CachedInputTokens, info.Last.Cached)
			addPtr(&cur.OutputTokens, info.Last.Output)
			cur.MeasuredAt = at
		}
	}
	flush()
	return out, found, nil
}

// OpenThreadRollout opens a thread's rollout from the account's session
// storage without following symlinks or blocking on a FIFO writer.
func OpenThreadRollout(home string, thread Thread, additionalHomes ...string) (*os.File, error) {
	return openAccountRollout(append([]string{home}, additionalHomes...), thread.RolloutPath)
}

// UsageInFile is Usage over an opened rollout, which must be a regular file.
func UsageInFile(f *os.File) (SessionUsage, bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return SessionUsage{}, false, err
	}
	if !fi.Mode().IsRegular() {
		return SessionUsage{}, false, fmt.Errorf("codexctx: rollout is not a regular file")
	}
	return Usage(f, fi.Size())
}
