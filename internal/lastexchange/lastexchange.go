// Package lastexchange extracts the user's last genuine message, plus the
// session's own reply to it, from a Claude Code JSONL transcript so a recycled
// (reset) session can be told what was still open with the human.
//
// It is deliberately pure (no agent/config/db imports): callers locate the
// transcript file and pass its path, so both recycle seed-prompt builders
// (internal/hera and internal/daemon) share one implementation without an
// import cycle. Everything here fails open — a read or parse problem yields an
// empty Exchange, never an error that could block a recycle.
package lastexchange

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxHumanBytes / MaxReplyBytes cap what is copied into the seed prompt.
	MaxHumanBytes = 2 << 10
	MaxReplyBytes = 4 << 10

	// maxLineBytes bounds a single transcript line we are willing to parse;
	// larger lines (giant tool results) are skipped, not buffered.
	maxLineBytes = 8 << 20

	midTurnMarker = "The user sent a new message while you were working:"
	pluginPrefix  = "The argus-bridge plugin sent a message"
	deliveryTag   = "[argus-delivery:"
)

// RelayDirective is appended to a recycle seed prompt so the fresh session
// surfaces anything still open with the human before doing other work.
const RelayDirective = "\n## Open with the user — relay first\n" +
	"If the handoff note has a non-empty \"Open with the user\" section (anything other than \"none\"), " +
	"or a \"Last exchange with the user before the reset\" section appears in this prompt, your FIRST user-visible " +
	"reply must relay it: give the user the answer they were owed, or ask the question you still owe them, " +
	"before any other work. Keep it short and do not describe the reset itself.\n"

// Exchange is the last genuine human message and the assistant's reply text
// after it. Either field may be empty.
type Exchange struct {
	Human string
	Reply string
}

var (
	reminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	hookTagRe  = regexp.MustCompile(`(?s)<(user-prompt-submit-hook|local-command-stdout|local-command-caveat)>.*?</(user-prompt-submit-hook|local-command-stdout|local-command-caveat)>`)
)

type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"` // tool_result payload: string or []block
}

type line struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// FromFile reads the transcript at path. A missing/unreadable file returns an
// empty Exchange and the error (callers log it and omit the section).
func FromFile(path string) (Exchange, error) {
	f, err := os.Open(path) //nolint:gosec // path is built from daemon-known task state
	if err != nil {
		return Exchange{}, err
	}
	defer f.Close()
	return Extract(f)
}

// Extract scans JSONL from r in order, remembering the latest genuine human
// message and every assistant text block that follows it.
func Extract(r io.Reader) (Exchange, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var (
		human   string
		replies []string
	)
	for {
		raw, err := readLine(br)
		if len(raw) > 0 {
			isUser := bytes.Contains(raw, []byte(`"type":"user"`))
			isAsst := bytes.Contains(raw, []byte(`"type":"assistant"`))
			if isUser || isAsst {
				var l line
				if json.Unmarshal(raw, &l) == nil && !l.IsSidechain {
					switch {
					case l.Type == "user" && !l.IsMeta:
						if h := humanFromUser(l.Message.Content); h != "" {
							human, replies = h, nil
						}
					case l.Type == "assistant" && human != "":
						replies = append(replies, assistantText(l.Message.Content)...)
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return finish(human, replies), err
		}
	}
	return finish(human, replies), nil
}

func finish(human string, replies []string) Exchange {
	if human == "" {
		return Exchange{}
	}
	return Exchange{
		Human: truncateKeepStart(human, MaxHumanBytes),
		Reply: truncateKeepEnd(strings.Join(replies, "\n\n"), MaxReplyBytes),
	}
}

// readLine returns the next line, dropping (returning nil for) any line longer
// than maxLineBytes while still consuming it.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	over := false
	for {
		part, err := br.ReadSlice('\n')
		if !over {
			if len(buf)+len(part) > maxLineBytes {
				over, buf = true, nil
			} else {
				buf = append(buf, part...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if over {
			return nil, err
		}
		return buf, err
	}
}

func assistantText(raw json.RawMessage) []string {
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
			return []string{strings.TrimSpace(s)}
		}
		return nil
	}
	var out []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			out = append(out, strings.TrimSpace(b.Text))
		}
	}
	return out
}

// humanFromUser returns the genuine human text carried by a user-role entry,
// or "" when the entry is plugin-delivered, hook/system-only, empty, or a bare
// tool_result. A human message surfaced mid-turn inside a tool_result (wrapped
// by Claude Code) is unwrapped and returned.
func humanFromUser(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanHuman(s)
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	midTurn := ""
	for _, b := range blocks {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_result":
			if m := midTurnText(b.Content); m != "" {
				midTurn = m
			}
		}
	}
	if h := cleanHuman(strings.Join(texts, "\n")); h != "" {
		return h
	}
	return midTurn
}

func midTurnText(raw json.RawMessage) string {
	var parts []string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		parts = []string{s}
	} else {
		var blocks []contentBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
	}
	for _, p := range parts {
		i := strings.Index(p, midTurnMarker)
		if i < 0 {
			continue
		}
		rest := p[i+len(midTurnMarker):]
		if j := strings.Index(rest, "\n\nIMPORTANT:"); j >= 0 {
			rest = rest[:j]
		}
		if h := cleanHuman(rest); h != "" {
			return h
		}
	}
	return ""
}

func cleanHuman(s string) string {
	s = reminderRe.ReplaceAllString(s, "")
	s = hookTagRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	switch {
	case s == "",
		strings.HasPrefix(s, pluginPrefix),
		strings.Contains(s, deliveryTag),
		strings.HasPrefix(s, "[Request interrupted by user"):
		return ""
	}
	return s
}

func truncateKeepStart(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[…truncated]"
}

func truncateKeepEnd(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "[…truncated]\n" + s[cut:]
}

// Section renders the seed-prompt section, or "" when there is no human
// message to relay.
func (e Exchange) Section() string {
	if e.Human == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Last exchange with the user before the reset\n")
	b.WriteString("(Extracted automatically from your prior session's transcript.)\n\n")
	b.WriteString("**User's last message:**\n")
	b.WriteString(e.Human)
	b.WriteString("\n\n**Your prior session's reply after it:**\n")
	if e.Reply == "" {
		b.WriteString("(none — the user may not have received an answer)")
	} else {
		b.WriteString(e.Reply)
	}
	b.WriteString("\n")
	return b.String()
}
