package voice

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSpeakBrief(t *testing.T) {
	for _, tc := range []struct {
		name string
		snap Snapshot
		want string
	}{
		{
			name: "quiet",
			want: "Nothing is running. Nothing finished in the last half hour.",
		},
		{
			name: "one running",
			snap: Snapshot{Counts: Counts{Running: 1}, Running: []string{"Review"}},
			want: "Review is running.",
		},
		{
			name: "running and idle",
			snap: Snapshot{Counts: Counts{Running: 2, Idle: 1}, Running: []string{"A", "B"}, Idle: []string{"C"}},
			want: "Two tasks are running and one is idle.",
		},
		{
			name: "needs input and blocked and finished",
			snap: Snapshot{
				Counts:           Counts{NeedsInput: 1, Blocked: 1, RecentlyFinished: 1},
				NeedsInput:       []string{"Review"},
				Blocked:          []string{"Deploy"},
				RecentlyFinished: []string{"Docs"},
			},
			want: "Nothing is running. Review needs your input, and Deploy is blocked. Docs finished recently.",
		},
		{
			name: "several finished",
			snap: Snapshot{Counts: Counts{RecentlyFinished: 3}, RecentlyFinished: []string{"A", "B", "C"}},
			want: "Nothing is running. Three tasks finished recently.",
		},
		{
			name: "markdown name",
			snap: Snapshot{Counts: Counts{Running: 1}, Running: []string{"`*Review*`\n"}},
			want: "Review is running.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SpeakBrief(tc.snap)
			testutil.Equal(t, got, tc.want)
			testutil.False(t, strings.ContainsAny(got, "`*#_[]"))
			testutil.False(t, strings.Contains(got, "\n"))
			sentences := strings.Count(got, ".")
			testutil.True(t, sentences >= 1 && sentences <= 3)
		})
	}
}

func TestSpeakName(t *testing.T) {
	testutil.Equal(t, SpeakName("  "), "a task")
	long := strings.Repeat("a", 50)
	testutil.Equal(t, len([]rune(SpeakName(long))), 40)
}

func TestCommandsExcludeKnowledgeBaseWrites(t *testing.T) {
	for _, c := range Commands() {
		testutil.False(t, c.Name == "kb_ingest" || c.Name == "kb_delete")
		for _, arg := range c.Args {
			testutil.True(t, arg.Name != "" && arg.Name == strings.ToLower(arg.Name))
		}
	}
	testutil.Equal(t, len(Commands()), 8)
}
