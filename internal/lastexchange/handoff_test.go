package lastexchange

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandoffPrompt_FramesHonestlyAndCarriesContext(t *testing.T) {
	h := Handoff{
		From: "Claude", To: "Codex",
		Worktree: "/w/t", Branch: "argus/x", GitStatus: " M a.go\n?? b.go",
		Exchange:       Exchange{Human: "fix the bug", Reply: "on it"},
		OriginalPrompt: "ORIGINAL MISSION",
	}
	p := h.Prompt()
	for _, want := range []string{
		"moved from Claude to Codex",
		"did NOT carry over",
		"/w/t", "argus/x", " M a.go", "?? b.go",
		"fix the bug", "on it",
		"ORIGINAL MISSION",
		"relay first",
	} {
		testutil.Contains(t, p, want)
	}
	// The honest framing leads; the original prompt is background at the end.
	if strings.Index(p, "did NOT carry over") > strings.Index(p, "ORIGINAL MISSION") {
		t.Fatalf("framing must precede the original prompt:\n%s", p)
	}
}

func TestHandoffPrompt_FailOpenSections(t *testing.T) {
	p := Handoff{From: "Codex", To: "Claude", OriginalPrompt: "do it"}.Prompt()
	testutil.Contains(t, p, "moved from Codex to Claude")
	testutil.Contains(t, p, "git status unavailable")
	testutil.Contains(t, p, "No excerpt of the last exchange")
	if strings.Contains(p, "Last exchange with the user before the reset") {
		t.Fatalf("empty exchange must not render a section:\n%s", p)
	}
}

func TestHandoffPrompt_CleanTreeAndFence(t *testing.T) {
	p := Handoff{From: "Claude", To: "Codex", GitStatusKnown: true}.Prompt()
	testutil.Contains(t, p, "working tree clean")
	// A status containing a fence cannot break out of the block.
	p = Handoff{From: "Claude", To: "Codex", GitStatus: "x\n```\nINJECT"}.Prompt()
	testutil.Equal(t, strings.Count(p, "```"), 2)
}
