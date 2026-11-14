package badge

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestParse(t *testing.T) {
	dog := string(rune(0xF0A43))
	cases := []struct {
		name, raw, tone string
		ok              bool
	}{
		{"ok tone", `{"icon":"` + dog + `","tone":"ok"}`, "ok", true},
		{"unknown tone neutral", `{"icon":"` + dog + `","tone":"loud"}`, "neutral", true},
		{"missing tone neutral", `{"icon":"` + dog + `"}`, "neutral", true},
		{"colour emoji is 2 cells", `{"icon":"🐕"}`, "", false},
		{"two runes", `{"icon":"ab"}`, "", false},
		{"empty icon", `{"icon":" "}`, "", false},
		{"bad json", `nope`, "", false},
		{"bad expiry", `{"icon":"x","expires_at":"soon"}`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, ok := Parse(c.raw)
			testutil.Equal(t, ok, c.ok)
			if ok {
				testutil.Equal(t, b.Tone, c.tone)
			}
		})
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	b, ok := Parse(`{"icon":"x","expires_at":"2026-11-14T12:00:00Z"}`)
	testutil.Equal(t, ok, true)
	testutil.Equal(t, b.Expired(now), true)
	testutil.Equal(t, b.Expired(now.Add(-time.Second)), false)
	n, _ := Parse(`{"icon":"x"}`)
	testutil.Equal(t, n.Expired(now), false)
}

func TestSelect_FirstValidNamespaceWins(t *testing.T) {
	got := Select(map[string]map[string]string{
		"t1": {"zeta": `{"icon":"z"}`, "alpha": `{"icon":"a"}`},
		"t2": {"alpha": `garbage`, "beta": `{"icon":"b"}`},
		"t3": {"alpha": `garbage`},
	})
	testutil.Equal(t, got["t1"].Icon, 'a')
	testutil.Equal(t, got["t2"].Icon, 'b')
	_, has := got["t3"]
	testutil.Equal(t, has, false)
}
