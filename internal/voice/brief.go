// Package voice maps short spoken commands onto Argus task operations and
// turns daemon state into a few plain sentences a voice client can read aloud.
package voice

import (
	"strings"
	"time"
	"unicode"
)

// RecentWindow is how far back a completed task still counts as "finished
// recently" in a spoken brief.
const RecentWindow = 30 * time.Minute

// Counts is the structured half of a spoken brief. Running, idle, needs_input,
// blocked and recently_finished are the voice buckets. sessions_running and
// sessions_idle match GET /api/status (sessions_running includes idle
// sessions). The task status counters match that endpoint's task counts.
type Counts struct {
	Running          int `json:"running"`
	Idle             int `json:"idle"`
	NeedsInput       int `json:"needs_input"`
	Blocked          int `json:"blocked"`
	RecentlyFinished int `json:"recently_finished"`
	SessionsRunning  int `json:"sessions_running"`
	SessionsIdle     int `json:"sessions_idle"`
	Pending          int `json:"pending"`
	InProgress       int `json:"in_progress"`
	InReview         int `json:"in_review"`
	Complete         int `json:"complete"`
}

// Snapshot is the input to SpeakBrief. Name slices line up with the voice
// buckets in Counts. SessionsRunning and SessionsIdle are the raw /api/status
// session totals.
type Snapshot struct {
	Counts           Counts
	Running          []string
	Idle             []string
	NeedsInput       []string
	Blocked          []string
	RecentlyFinished []string
	SessionsRunning  int
	SessionsIdle     int
}

// SpeakBrief renders one to three plain sentences. No markdown, no lists.
func SpeakBrief(s Snapshot) string {
	parts := []string{sentence(activityClause(s))}
	if attn := attentionClause(s); attn != "" {
		parts = append(parts, sentence(attn))
	}
	if fin := finishedClause(s); fin != "" {
		parts = append(parts, sentence(fin))
	} else if quiet(s) {
		parts = append(parts, "Nothing finished in the last half hour.")
	}
	return strings.Join(parts, " ")
}

func quiet(s Snapshot) bool {
	c := s.Counts
	return c.Running == 0 && c.Idle == 0 && c.NeedsInput == 0 && c.Blocked == 0 && c.RecentlyFinished == 0
}

func activityClause(s Snapshot) string {
	rn, idle := s.Counts.Running, s.Counts.Idle
	switch {
	case rn == 0 && idle == 0:
		return "nothing is running"
	case rn == 1 && idle == 0:
		return SpeakName(first(s.Running)) + " is running"
	case rn == 0 && idle == 1:
		return SpeakName(first(s.Idle)) + " is idle"
	case idle == 0:
		return qty(rn) + " " + plural(rn, "task", "tasks") + " are running"
	case rn == 0:
		return qty(idle) + " " + plural(idle, "task", "tasks") + " are idle"
	default:
		return qty(rn) + " " + plural(rn, "task is", "tasks are") + " running and " + qty(idle) + " " + plural(idle, "is", "are") + " idle"
	}
}

func attentionClause(s Snapshot) string {
	ni, bl := s.Counts.NeedsInput, s.Counts.Blocked
	if ni == 0 && bl == 0 {
		return ""
	}
	var b strings.Builder
	if ni == 1 {
		b.WriteString(SpeakName(first(s.NeedsInput)) + " needs your input")
	} else if ni > 1 {
		b.WriteString(nameList(s.NeedsInput) + " need your input")
	}
	if bl > 0 {
		if ni > 0 {
			b.WriteString(", and ")
		}
		if bl == 1 {
			b.WriteString(SpeakName(first(s.Blocked)) + " is blocked")
		} else {
			b.WriteString(nameList(s.Blocked) + " are blocked")
		}
	}
	return b.String()
}

func finishedClause(s Snapshot) string {
	n := s.Counts.RecentlyFinished
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return SpeakName(first(s.RecentlyFinished)) + " finished recently"
	case n == 2:
		return nameList(s.RecentlyFinished) + " finished recently"
	default:
		return qty(n) + " tasks finished recently"
	}
}

// SpeakName flattens a task name into something that can sit inside a spoken
// sentence: no control characters, no markdown markers, collapsed whitespace.
func SpeakName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("`*_#[]<>~\\|+-!?.", r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "a task"
	}
	rs := []rune(name)
	if len(rs) > 40 {
		return string(rs[:40])
	}
	return name
}

func nameList(names []string) string {
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		cleaned = append(cleaned, SpeakName(n))
	}
	switch len(cleaned) {
	case 0:
		return "a task"
	case 1:
		return cleaned[0]
	case 2:
		return cleaned[0] + " and " + cleaned[1]
	default:
		return cleaned[0] + ", " + cleaned[1] + ", and " + qty(len(cleaned)-2) + " more"
	}
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func qty(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	s = string(rs)
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
