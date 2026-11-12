package transcriptstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func classify(t *testing.T, lines ...string) State {
	t.Helper()
	st, err := Classify(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	testutil.NoError(t, err)
	return st
}

const (
	human      = `{"type":"user","message":{"role":"user","content":"hi"}}`
	asstTool   = `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`
	toolResult = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`
	asstEnd    = `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`
	asstThink  = `{"type":"assistant","message":{"role":"assistant","stop_reason":null,"content":[{"type":"thinking","thinking":"hmm"}]}}`
	askUse     = `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"question":"Where should it run?","options":[{"label":"Headless"},{"label":"Cloud"}]}]}}]}}`
	askResult  = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"q1","content":"Headless"}]}}`
	interrupt  = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`
	interruptT = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`
	interruptS = `{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"}}`
	trailer    = `{"type":"last-prompt","lastPrompt":"x"}`
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  Kind
	}{
		{"empty", nil, Unknown},
		{"parked question", []string{human, asstTool, toolResult, askUse, trailer}, ParkedQuestion},
		{"answered question is mid turn", []string{human, askUse, askResult}, MidTurn},
		{"tool call", []string{human, asstTool}, MidTurn},
		{"tool result", []string{human, asstTool, toolResult}, MidTurn},
		{"thinking only", []string{human, asstThink}, MidTurn},
		{"user message unanswered", []string{human}, MidTurn},
		{"end of turn", []string{human, asstEnd, trailer}, Idle},
		{"esc interrupt is idle", []string{human, asstTool, interrupt}, Idle},
		{"esc interrupt for tool use is idle", []string{human, asstTool, toolResult, interruptT}, Idle},
		{"esc interrupt as plain string is idle", []string{human, asstThink, interruptS}, Idle},
		{"esc dismisses an open question", []string{human, askUse, interrupt}, Idle},
		{"work after an interrupt is mid turn again", []string{human, interrupt, human, asstTool}, MidTurn},
		{"sidechain ignored", []string{human, asstEnd, `{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"tool_use","id":"s","name":"Bash","input":{}}]}}`}, Idle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, classify(t, tt.lines...).Kind, tt.want)
		})
	}
}

func TestClassify_QuestionText(t *testing.T) {
	st := classify(t, human, askUse)
	testutil.Equal(t, st.Question, "Where should it run?")
	testutil.DeepEqual(t, st.Options, []string{"Headless", "Cloud"})
}

func TestClassify_OptionsAreOneLine(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"q","name":"AskUserQuestion","input":{"questions":[{"question":"Q","options":[{"label":"multi\nline   label"}]}]}}]}}`
	st := classify(t, human, line)
	testutil.DeepEqual(t, st.Options, []string{"multi line label"})
}

func TestClassify_ExitPlanMode(t *testing.T) {
	plan := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"p","name":"ExitPlanMode","input":{"plan":"do x"}}]}}`
	st := classify(t, human, plan)
	testutil.Equal(t, st.Kind, ParkedQuestion)
	testutil.Contains(t, st.Question, "do x")
}

func TestClassify_CapsQuestion(t *testing.T) {
	q := strings.Repeat("a", 5000) + "\x1b[31m"
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"q","name":"AskUserQuestion","input":{"questions":[{"question":"` + q[:5000] + `\u001b[31m"}]}}]}}`
	st := classify(t, human, line)
	testutil.Equal(t, len(st.Question) <= MaxQuestionBytes+len("[…truncated]"), true)
	testutil.Equal(t, strings.ContainsRune(st.Question, 0x1b), false)
}

func TestFromFile_TailOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	// A huge head the tail window must skip, then a parked question.
	pad := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 1<<20) + `"}}` + "\n"
	var b strings.Builder
	for i := 0; i < 6; i++ {
		b.WriteString(pad)
	}
	b.WriteString(askUse + "\n")
	testutil.NoError(t, os.WriteFile(p, []byte(b.String()), 0o600))
	st, err := FromFile(p)
	testutil.NoError(t, err)
	testutil.Equal(t, st.Kind, ParkedQuestion)
}

func TestFromFile_Missing(t *testing.T) {
	st, err := FromFile(filepath.Join(t.TempDir(), "nope.jsonl"))
	testutil.Equal(t, err != nil, true)
	testutil.Equal(t, st.Kind, Unknown)
}
