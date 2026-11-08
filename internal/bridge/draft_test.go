package bridge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func draftEv(seq int64, boot, text string, cursor int) Event {
	return Event{Type: TypeDraft, Seq: seq, Boot: boot, SessionID: "s1", Text: text, Cursor: cursor}
}

func TestHub_DraftReporting(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "fix the bug", 11)}, t0)
	st, _ := h.Get("t")
	testutil.True(t, st.HasDraft)

	h.Ingest("t", []Event{draftEv(2, "b1", "", 0)}, t0.Add(time.Second))
	st, _ = h.Get("t")
	testutil.False(t, st.HasDraft)
}

func TestHub_DraftTextNeverSerialized(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "super secret plan", 3)}, t0)
	st, _ := h.Get("t")
	raw, err := json.Marshal(st)
	testutil.NoError(t, err)
	testutil.True(t, strings.Contains(string(raw), `"has_draft":true`))
	testutil.False(t, strings.Contains(string(raw), "secret"))
}

func TestHub_TakeDraft(t *testing.T) {
	tests := []struct {
		name   string
		events []Event
		boot   string
		at     time.Time
		want   bool
	}{
		{"new boot gets draft", []Event{draftEv(1, "b1", "hello", 5)}, "b2", t0.Add(5 * time.Second), true},
		{"same boot gets nothing", []Event{draftEv(1, "b1", "hello", 5)}, "b1", t0.Add(5 * time.Second), false},
		{"empty draft gets nothing", []Event{draftEv(1, "b1", "hello", 5), draftEv(2, "b1", "", 0)}, "b2", t0.Add(5 * time.Second), false},
		{"stale draft gets nothing", []Event{draftEv(1, "b1", "hello", 5)}, "b2", t0.Add(DraftMaxAge + time.Second), false},
		{"just inside the window", []Event{draftEv(1, "b1", "hello", 5)}, "b2", t0.Add(DraftMaxAge - time.Second), true},
		{"no draft at all", nil, "b2", t0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHub()
			h.Ingest("t", tc.events, t0)
			d, ok := h.TakeDraft("t", tc.boot, tc.at)
			testutil.Equal(t, ok, tc.want)
			if ok {
				testutil.Equal(t, d.Text, "hello")
				testutil.Equal(t, d.Cursor, 5)
			}
		})
	}
}

func TestHub_TakeDraftIsOneShot(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "hello", 5)}, t0)
	_, ok := h.TakeDraft("t", "b2", t0.Add(time.Second))
	testutil.True(t, ok)
	_, ok = h.TakeDraft("t", "b2", t0.Add(time.Second))
	testutil.False(t, ok)
	_, ok = h.TakeDraft("t", "b3", t0.Add(time.Second))
	testutil.False(t, ok)
}

// A restarted mod boots with a new Boot id; the previous process's draft must
// survive that reset (it is the very thing being handed over) while the new
// boot's own state starts without a draft.
func TestHub_DraftSurvivesNewBoot(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "hello", 5)}, t0)
	h.Ingest("t", []Event{{Type: TypeSessionStart, Seq: 1, Boot: "b2", SessionID: "s1"}}, t0.Add(2*time.Second))
	st, _ := h.Get("t")
	testutil.False(t, st.HasDraft)
	d, ok := h.TakeDraft("t", "b2", t0.Add(3*time.Second))
	testutil.True(t, ok)
	testutil.Equal(t, d.Text, "hello")
}

// The new boot's own typing before the pull arrives must win: a draft reported
// by the requesting boot is never handed back to itself.
func TestHub_TakeDraftIgnoresOwnBoot(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "old", 3)}, t0)
	h.Ingest("t", []Event{draftEv(1, "b2", "typed", 5)}, t0.Add(time.Second))
	_, ok := h.TakeDraft("t", "b2", t0.Add(2*time.Second))
	testutil.False(t, ok)
}

func TestHub_PruneDropsOldDrafts(t *testing.T) {
	h := NewHub()
	h.Ingest("t", []Event{draftEv(1, "b1", "hello", 5)}, t0)
	h.Prune(t0.Add(2 * time.Hour))
	_, ok := h.TakeDraft("t", "b2", t0.Add(2*time.Hour))
	testutil.False(t, ok)
	testutil.Equal(t, len(h.drafts), 0)
}
