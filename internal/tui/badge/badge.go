// Package badge parses the generic plugin task badge: a task_meta row with key
// "badge" in any namespace whose value is a small JSON object. Argus knows
// nothing about which plugin wrote it; the TUI just draws the glyph.
package badge

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"github.com/argus-foundry/argus/internal/tui/theme"
)

// Key is the task_meta key a plugin writes (in its own namespace).
const Key = "badge"

// Badge is a parsed, valid badge.
type Badge struct {
	Icon      rune
	Tone      string
	ExpiresAt time.Time // zero = never
}

type wire struct {
	Icon      string `json:"icon"`
	Tone      string `json:"tone"`
	ExpiresAt string `json:"expires_at"`
}

// Parse decodes one badge value. It reports false for invalid JSON, an icon
// that is not exactly one rune and one cell wide, or an unparseable
// expires_at. Unknown or empty tones fall back to neutral.
func Parse(raw string) (Badge, bool) {
	var w wire
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return Badge{}, false
	}
	icon := []rune(strings.TrimSpace(w.Icon))
	if len(icon) != 1 || runewidth.RuneWidth(icon[0]) != 1 {
		return Badge{}, false
	}
	b := Badge{Icon: icon[0], Tone: w.Tone}
	switch w.Tone {
	case "ok", "warn", "alert":
	default:
		b.Tone = "neutral"
	}
	if w.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, w.ExpiresAt)
		if err != nil {
			return Badge{}, false
		}
		b.ExpiresAt = t
	}
	return b, true
}

// Expired reports whether the badge's expires_at has passed at now.
func (b Badge) Expired(now time.Time) bool {
	return !b.ExpiresAt.IsZero() && !now.Before(b.ExpiresAt)
}

// Style maps the tone onto an existing theme style.
func (b Badge) Style() tcell.Style {
	switch b.Tone {
	case "ok":
		return theme.StyleComplete
	case "warn":
		return theme.StyleInReview
	case "alert":
		return theme.StyleError
	}
	return theme.StyleDimmed
}

// Select reduces taskID → namespace → raw value to one badge per task: the
// valid badge from the alphabetically first namespace that has one.
func Select(raw map[string]map[string]string) map[string]Badge {
	out := make(map[string]Badge, len(raw))
	for taskID, byNS := range raw {
		nss := make([]string, 0, len(byNS))
		for ns := range byNS {
			nss = append(nss, ns)
		}
		sort.Strings(nss)
		for _, ns := range nss {
			if b, ok := Parse(byNS[ns]); ok {
				out[taskID] = b
				break
			}
		}
	}
	return out
}

// Reader is the optional store capability the TUI type-asserts for; local
// *db.DB implements it, the remote apistore deliberately does not.
type Reader interface {
	ListMetaByKey(key string) (map[string]map[string]string, error)
}
