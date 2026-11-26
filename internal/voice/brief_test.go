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
		{
			name: "one idle",
			snap: Snapshot{Counts: Counts{Idle: 1}, Idle: []string{"Docs"}},
			want: "Docs is idle.",
		},
		{
			name: "many idle",
			snap: Snapshot{Counts: Counts{Idle: 10}},
			want: "10 tasks are idle.",
		},
		{
			name: "two needs and two blocked",
			snap: Snapshot{
				Counts:     Counts{NeedsInput: 2, Blocked: 2},
				NeedsInput: []string{"Review", "Docs"},
				Blocked:    []string{"Deploy", "Ship"},
			},
			want: "Nothing is running. Review and Docs need your input, and Deploy and Ship are blocked.",
		},
		{
			name: "three finished by name",
			snap: Snapshot{
				Counts:           Counts{Running: 1, RecentlyFinished: 2},
				Running:          []string{"Api"},
				RecentlyFinished: []string{"Docs", "Notes"},
			},
			want: "Api is running. Docs and Notes finished recently.",
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
	testutil.Equal(t, nameList(nil), "a task")
	testutil.Equal(t, nameList([]string{"A", "B", "C"}), "A, B, and one more")
	testutil.Equal(t, itoa(-12), "-12")
	testutil.Equal(t, itoa(0), "0")
	testutil.Equal(t, sentence(""), "")
	testutil.Equal(t, first(nil), "")
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
