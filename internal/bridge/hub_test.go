package bridge

import (
	"math"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func ev(typ string, seq int64) Event { return Event{Type: typ, Seq: seq, Boot: "b1", SessionID: "s1"} }

func TestHub_StateMachine(t *testing.T) {
	cost := 1.5
	tokens := int64(1000)
	tests := []struct {
		name   string
		events []Event
		check  func(t *testing.T, st State)
	}{
		{"turn start is busy", []Event{ev(TypeTurnStart, 1)}, func(t *testing.T, st State) {
			testutil.True(t, st.Busy)
		}},
		{"turn complete is idle", []Event{ev(TypeTurnStart, 1), ev(TypeTurnComplete, 2)}, func(t *testing.T, st State) {
			testutil.False(t, st.Busy)
		}},
		{"needs input then cleared", []Event{
			ev(TypeTurnStart, 1),
			{Type: TypeNeedsInput, Seq: 2, Boot: "b1", Kind: KindQuestion},
		}, func(t *testing.T, st State) {
			testutil.True(t, st.NeedsInput)
			testutil.Equal(t, st.NeedsInputKind, KindQuestion)
		}},
		{"cleared drops needs input", []Event{
			{Type: TypeNeedsInput, Seq: 1, Boot: "b1", Kind: KindPlan},
			ev(TypeCleared, 2),
		}, func(t *testing.T, st State) {
			testutil.False(t, st.NeedsInput)
			testutil.Equal(t, st.NeedsInputKind, "")
		}},
		{"turn complete clears a stuck dialog", []Event{
			ev(TypeTurnStart, 1),
			{Type: TypeNeedsInput, Seq: 2, Boot: "b1", Kind: KindPermission},
			ev(TypeTurnComplete, 3),
		}, func(t *testing.T, st State) {
			testutil.False(t, st.NeedsInput)
		}},
		{"session start records id model cwd", []Event{
			{Type: TypeSessionStart, Seq: 1, Boot: "b1", SessionID: "abc", Model: "opus", Cwd: "/w"},
		}, func(t *testing.T, st State) {
			testutil.Equal(t, st.SessionID, "abc")
			testutil.Equal(t, st.Model, "opus")
			testutil.Equal(t, st.Cwd, "/w")
		}},
		{"session id follows the newest event", []Event{
			{Type: TypeSessionStart, Seq: 1, Boot: "b1", SessionID: "old"},
			{Type: TypeSessionStart, Seq: 2, Boot: "b1", SessionID: "new"},
		}, func(t *testing.T, st State) {
			testutil.Equal(t, st.SessionID, "new")
		}},
		{"measure stored", []Event{
			{Type: TypeMeasure, Seq: 1, Boot: "b1", Context: &Context{Tokens: &tokens, Window: 200000}, CostUSD: &cost,
				RateLimits: []RateLimit{{Kind: "five_hour", PercentUsed: 40}}},
		}, func(t *testing.T, st State) {
			testutil.Equal(t, *st.Context.Tokens, int64(1000))
			testutil.Equal(t, *st.CostUSD, 1.5)
			testutil.Equal(t, len(st.RateLimits), 1)
		}},
		{"heartbeat stamps", []Event{ev(TypeHeartbeat, 1)}, func(t *testing.T, st State) {
			testutil.False(t, st.LastHeartbeatAt.IsZero())
			testutil.False(t, st.Busy)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHub()
			testutil.Equal(t, h.Ingest("t", tt.events, t0), len(tt.events))
			st, ok := h.Get("t")
			testutil.True(t, ok)
			tt.check(t, st)
		})
	}
}

func TestHub_Dedupe(t *testing.T) {
	h := NewHub()
	testutil.Equal(t, h.Ingest("t", []Event{ev(TypeTurnStart, 5)}, t0), 1)
	// Duplicate and lower seq in the same boot are dropped.
	testutil.Equal(t, h.Ingest("t", []Event{ev(TypeTurnComplete, 5), ev(TypeTurnComplete, 4)}, t0), 0)
	st, _ := h.Get("t")
	testutil.True(t, st.Busy)
	// A new boot (hot reload) restarts the seq baseline.
	e := ev(TypeTurnComplete, 1)
	e.Boot = "b2"
	testutil.Equal(t, h.Ingest("t", []Event{e}, t0), 1)
	st, _ = h.Get("t")
	testutil.False(t, st.Busy)
	testutil.Equal(t, st.Boot, "b2")
}

func TestHub_UnknownTypeRejected(t *testing.T) {
	h := NewHub()
	testutil.Equal(t, h.Ingest("t", []Event{ev("future.thing", 1)}, t0), 0)
	_, ok := h.Get("t")
	testutil.False(t, ok)
}

func TestHub_LiveAndPrune(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{ev(TypeHeartbeat, 1)}, t0)
	_, live := h.Live("t", t0.Add(LiveWindow))
	testutil.True(t, live)
	_, live = h.Live("t", t0.Add(LiveWindow+time.Second))
	testutil.False(t, live)
	_, live = h.Live("missing", t0)
	testutil.False(t, live)
	testutil.Equal(t, h.Prune(t0.Add(30*time.Minute)), 0)
	testutil.Equal(t, h.Prune(t0.Add(2*time.Hour)), 1)
	testutil.Equal(t, h.Len(), 0)
}

func TestHub_GetReturnsCopy(t *testing.T) {
	h := NewHub()
	cost := 2.0
	h.Ingest("t", []Event{{Type: TypeMeasure, Seq: 1, Boot: "b", CostUSD: &cost, Context: &Context{Window: 1},
		RateLimits: []RateLimit{{Kind: "x"}}}}, t0)
	st, _ := h.Get("t")
	*st.CostUSD = 99
	st.Context.Window = 99
	st.RateLimits[0].Kind = "mutated"
	again, _ := h.Get("t")
	testutil.Equal(t, *again.CostUSD, 2.0)
	testutil.Equal(t, again.Context.Window, int64(1))
	testutil.Equal(t, again.RateLimits[0].Kind, "x")
}

func TestHub_TransitionStamp(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{ev(TypeTurnStart, 1)}, t0)
	st, _ := h.Get("t")
	testutil.True(t, st.LastTransitionAt.Equal(t0))
	// A repeated turn.start is not a transition.
	h.Ingest("t", []Event{ev(TypeTurnStart, 2)}, t0.Add(time.Second))
	st, _ = h.Get("t")
	testutil.True(t, st.LastTransitionAt.Equal(t0))
}

func TestHub_NewBootResetsLiveState(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{
		ev(TypeTurnStart, 1),
		{Type: TypeNeedsInput, Seq: 2, Boot: "b1", Kind: KindPermission, Confidence: ConfidencePendingOrRunning},
	}, t0)
	st, _ := h.Get("t")
	testutil.True(t, st.Busy)
	testutil.True(t, st.NeedsInput)
	testutil.Equal(t, st.NeedsInputConfidence, ConfidencePendingOrRunning)

	// The mod restarts mid-turn; its first event (a session.start) must not
	// resurrect the old busy/dialog state.
	reload := Event{Type: TypeSessionStart, Seq: 1, Boot: "b2", SessionID: "s1"}
	testutil.Equal(t, h.Ingest("t", []Event{reload}, t0.Add(time.Minute)), 1)
	st, _ = h.Get("t")
	testutil.False(t, st.Busy)
	testutil.False(t, st.NeedsInput)
	testutil.Equal(t, st.NeedsInputKind, "")
	testutil.Equal(t, st.NeedsInputConfidence, "")
	testutil.Equal(t, st.Boot, "b2")

	// Same boot continues normally afterwards.
	h.Ingest("t", []Event{{Type: TypeTurnStart, Seq: 2, Boot: "b2"}}, t0.Add(time.Minute))
	st, _ = h.Get("t")
	testutil.True(t, st.Busy)
}

func TestContextValidateAndClone(t *testing.T) {
	neg := int64(-1)
	zero := int64(0)
	nan := math.NaN()
	posInf, negInf := math.Inf(1), math.Inf(-1)
	pct := 5.0
	for name, c := range map[string]*Context{
		"negative tokens":   {Tokens: &neg},
		"negative window":   {Window: -1},
		"positive infinity": {Percent: &posInf},
		"negative infinity": {Percent: &negInf},
		"nan percent":       {Percent: &nan},
		"negative category": {Categories: &ContextCategories{Skills: &neg}},
	} {
		t.Run(name, func(t *testing.T) { testutil.True(t, c.Validate() != nil) })
	}
	good := &Context{Tokens: &zero, Percent: &pct, Window: 10, Categories: &ContextCategories{Memory: &zero, Messages: &zero}}
	testutil.NoError(t, good.Validate())
	testutil.NoError(t, (*Context)(nil).Validate())
	cp := good.Clone()
	*cp.Categories.Memory = 9
	*cp.Tokens = 9
	testutil.Equal(t, *good.Categories.Memory, int64(0))
	testutil.Equal(t, *good.Tokens, int64(0))
	testutil.True(t, cp.Categories.Agents == nil)
	testutil.True(t, (*Context)(nil).Clone() == nil)
}

func TestContextHubCloneEveryPointer(t *testing.T) {
	values := []int64{1, 3, 4, 5, 6, 7, 8, 9}
	pct := 2.0
	c := &Context{Tokens: &values[0], Percent: &pct, Categories: &ContextCategories{Messages: &values[1], Memory: &values[2], Tools: &values[3], Agents: &values[4], Skills: &values[5], System: &values[6], Compaction: &values[7]}}
	h := NewHub()
	testutil.Equal(t, h.Ingest("t", []Event{{Type: TypeMeasure, Seq: 1, Context: c}}, time.Now()), 1)
	for i := range values {
		values[i] = 99
	}
	pct = 99
	for range 2 {
		st, ok := h.Get("t")
		testutil.True(t, ok)
		testutil.Equal(t, *st.Context.Percent, 2.0)
		*st.Context.Percent = 99
		cats := st.Context.Categories
		for i, ptr := range []*int64{st.Context.Tokens, cats.Messages, cats.Memory, cats.Tools, cats.Agents, cats.Skills, cats.System, cats.Compaction} {
			want := int64(i + 2)
			if i == 0 {
				want = 1
			}
			testutil.Equal(t, *ptr, want)
			*ptr = 99
		}
	}
}

func sugg(typ string, seq int64, gen, session, text string) Event {
	return Event{Type: typ, Seq: seq, Boot: "b1", Generation: gen, SessionID: session, Text: text}
}

func TestHub_Suggestion(t *testing.T) {
	tests := []struct {
		name   string
		events []Event
		want   string
	}{
		{"changed stores text with session identity", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "Run the tests")}, "Run the tests"},
		{"later suggestion replaces earlier", []Event{
			sugg(TypeSuggestionChanged, 1, "g", "s", "one"), sugg(TypeSuggestionChanged, 2, "g", "s", "two")}, "two"},
		{"explicit clear", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "x"), sugg(TypeSuggestionCleared, 2, "g", "s", "")}, ""},
		{"new turn clears", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "x"), sugg(TypeTurnStart, 2, "g", "s", "")}, ""},
		{"turn complete keeps it", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "x"), sugg(TypeTurnComplete, 2, "g", "s", "")}, "x"},
		{"session replacement clears", []Event{sugg(TypeSuggestionChanged, 1, "g", "s1", "x"), sugg(TypeHeartbeat, 2, "g", "s2", "")}, ""},
		{"generation change clears", []Event{sugg(TypeSuggestionChanged, 1, "g1", "s", "x"), sugg(TypeHeartbeat, 2, "g2", "s", "")}, ""},
		{"new boot clears", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "x"), {Type: TypeHeartbeat, Seq: 1, Boot: "b2", Generation: "g", SessionID: "s"}}, ""},
		{"invalid text never stored", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "  ")}, ""},
		{"no session identity never stored", []Event{sugg(TypeSuggestionChanged, 1, "g", "", "x")}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHub()
			h.Ingest("t", tt.events, t0)
			st, _ := h.Get("t")
			if tt.want == "" {
				testutil.True(t, st.Suggestion == nil)
				return
			}
			testutil.Equal(t, st.Suggestion.Text, tt.want)
			testutil.Equal(t, st.Suggestion.Generation, "g")
			testutil.Equal(t, st.Suggestion.SessionID, "s")
			testutil.True(t, st.Suggestion.RecordedAt.Equal(t0))
		})
	}
}

func TestHub_SuggestionCopyIsIndependent(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{sugg(TypeSuggestionChanged, 1, "g", "s", "keep")}, t0)
	st, _ := h.Get("t")
	st.Suggestion.Text = "mutated"
	again, _ := h.Get("t")
	testutil.Equal(t, again.Suggestion.Text, "keep")
}

func TestEvent_ValidateSuggestion(t *testing.T) {
	big := make([]byte, MaxSuggestionBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	tests := []struct {
		name string
		ev   Event
		ok   bool
	}{
		{"valid", Event{Type: TypeSuggestionChanged, Text: "go"}, true},
		{"exactly 4 KiB", Event{Type: TypeSuggestionChanged, Text: string(big[:MaxSuggestionBytes])}, true},
		{"oversized", Event{Type: TypeSuggestionChanged, Text: string(big)}, false},
		{"blank", Event{Type: TypeSuggestionChanged, Text: " \n"}, false},
		{"empty", Event{Type: TypeSuggestionChanged}, false},
		{"invalid utf8", Event{Type: TypeSuggestionChanged, Text: "a\xffb"}, false},
		{"NUL", Event{Type: TypeSuggestionChanged, Text: "a\x00b"}, false},
		{"other types are not checked", Event{Type: TypeTurnStart, Text: string(big)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.Equal(t, tt.ev.ValidateSuggestion() == nil, tt.ok)
		})
	}
}
