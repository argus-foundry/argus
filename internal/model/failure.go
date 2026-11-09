package model

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	FailureSourceTaskResult = "task_result"
	MaxFailureReasonBytes   = 4096
)

// TaskFailure is the REST/SSE projection of an explicit agent-reported failure.
type TaskFailure struct {
	Reason     string `json:"reason"`
	Source     string `json:"source"`
	RecordedAt string `json:"recorded_at,omitempty"`
}

// ParseTaskFailure recognises the documented Result convention
// {"failed":true,"reason":"..."}. Anything else, including malformed JSON,
// non-objects and a non-boolean failed flag, reports no failure.
func ParseTaskFailure(result string) (reason string, ok bool) {
	if strings.TrimSpace(result) == "" {
		return "", false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		return "", false
	}
	var failed bool
	if raw, has := obj["failed"]; !has || json.Unmarshal(raw, &failed) != nil || !failed {
		return "", false
	}
	if raw, has := obj["reason"]; has {
		var r string
		if json.Unmarshal(raw, &r) == nil {
			reason = truncateUTF8(r, MaxFailureReasonBytes)
		}
	}
	return reason, true
}

// FailureFromResult builds the failure projection for a stored Result. at is
// the persisted observation time; a zero time leaves recorded_at out rather
// than inventing one.
func FailureFromResult(result string, at time.Time) *TaskFailure {
	reason, ok := ParseTaskFailure(result)
	if !ok {
		return nil
	}
	f := &TaskFailure{Reason: reason, Source: FailureSourceTaskResult}
	if !at.IsZero() {
		f.RecordedAt = at.UTC().Format(time.RFC3339)
	}
	return f
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
