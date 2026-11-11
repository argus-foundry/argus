// Package transcriptstate classifies what a Claude session was doing when it
// died, from the tail of its JSONL transcript: parked on a question, mid-turn,
// or idle between turns. It is pure (no agent/db imports) and fails open: any
// read or parse problem yields Unknown, never a blocking error.
package transcriptstate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Kind is the classified state at death.
type Kind string

const (
	Unknown        Kind = "unknown"
	ParkedQuestion Kind = "parked_question"
	MidTurn        Kind = "mid_turn"
	Idle           Kind = "idle"
)

const (
	// tailBytes bounds how much of the transcript end is read.
	tailBytes = 4 << 20
	// maxLineBytes bounds a single parsed line; larger lines are skipped.
	maxLineBytes = 4 << 20
	// MaxQuestionBytes caps the stored question text.
	MaxQuestionBytes = 2 << 10
	// MaxOptions caps the stored option labels.
	MaxOptions = 6
)

// State is the classification plus, for ParkedQuestion, the pending question.
type State struct {
	Kind     Kind
	Question string
	Options  []string
}

type block struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string `json:"tool_use_id"`
}

type entry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// FromFile classifies the transcript at path, reading only its tail.
func FromFile(path string) (State, error) {
	f, err := os.Open(path) //nolint:gosec // path is built from daemon-known task state
	if err != nil {
		return State{Kind: Unknown}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return State{Kind: Unknown}, err
	}
	var r io.Reader = f
	if info.Size() > tailBytes {
		if _, err := f.Seek(info.Size()-tailBytes, io.SeekStart); err != nil {
			return State{Kind: Unknown}, err
		}
		br := bufio.NewReader(f)
		// Drop the partial first line the seek landed in.
		if _, err := br.ReadBytes('\n'); err != nil {
			return State{Kind: Unknown}, nil
		}
		r = br
	}
	return Classify(r)
}

// Classify scans JSONL from r in order and classifies the final state.
func Classify(r io.Reader) (State, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var (
		seen      bool
		lastKind  Kind
		pending   = map[string]block{} // unanswered tool_use by id
		pendOrder []string
	)
	for {
		raw, err := readLine(br)
		if len(raw) > 0 {
			isUser := bytes.Contains(raw, []byte(`"type":"user"`))
			isAsst := bytes.Contains(raw, []byte(`"type":"assistant"`))
			if isUser || isAsst {
				var e entry
				if json.Unmarshal(raw, &e) == nil && !e.IsSidechain && (e.Type == "user" || e.Type == "assistant") {
					seen = true
					var blocks []block
					_ = json.Unmarshal(e.Message.Content, &blocks) // string content leaves blocks empty
					if e.Type == "assistant" {
						lastKind = MidTurn
						if e.Message.StopReason == "end_turn" {
							lastKind = Idle
						}
						for _, b := range blocks {
							if b.Type == "tool_use" {
								pending[b.ID] = b
								pendOrder = append(pendOrder, b.ID)
								lastKind = MidTurn
							}
						}
					} else {
						lastKind = MidTurn
						for _, b := range blocks {
							if b.Type == "tool_result" {
								delete(pending, b.ToolUseID)
							}
						}
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return State{Kind: Unknown}, err
		}
	}
	if !seen {
		return State{Kind: Unknown}, nil
	}
	// The newest unanswered question-class tool call wins.
	for i := len(pendOrder) - 1; i >= 0; i-- {
		b, ok := pending[pendOrder[i]]
		if !ok {
			continue
		}
		switch b.Name {
		case "AskUserQuestion":
			q, opts := askQuestion(b.Input)
			return State{Kind: ParkedQuestion, Question: q, Options: opts}, nil
		case "ExitPlanMode":
			return State{Kind: ParkedQuestion, Question: planQuestion(b.Input)}, nil
		}
	}
	return State{Kind: lastKind}, nil
}

func askQuestion(raw json.RawMessage) (string, []string) {
	var in struct {
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(raw, &in) != nil || len(in.Questions) == 0 {
		return "", nil
	}
	var qs []string
	var opts []string
	for _, q := range in.Questions {
		qs = append(qs, q.Question)
		for _, o := range q.Options {
			if len(opts) < MaxOptions {
				opts = append(opts, clean(o.Label, 200))
			}
		}
	}
	return clean(strings.Join(qs, "\n"), MaxQuestionBytes), opts
}

func planQuestion(raw json.RawMessage) string {
	var in struct {
		Plan string `json:"plan"`
	}
	_ = json.Unmarshal(raw, &in)
	return clean("Plan awaiting the user's approval (ExitPlanMode): "+in.Plan, MaxQuestionBytes)
}

// clean strips control characters (keeping newline/tab) and caps to n bytes.
func clean(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			return r
		}
		return -1
	}, s)
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "[…truncated]"
}

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
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if over {
			return nil, err
		}
		return buf, err
	}
}
