package backendtier

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/usagebudget"
)

const (
	WindowFiveHour = "five_hour"
	WindowSevenDay = "seven_day"

	codexShortWindowMinutes = 300
	rolloutTailBytes        = 2 << 20
)

// UsageWindow is one measured quota window. A window exists only when it was
// measured, so a PercentUsed of 0 is a real zero.
type UsageWindow struct {
	Kind        string    `json:"kind"`
	PercentUsed float64   `json:"percent_used"`
	ResetsAt    time.Time `json:"resets_at"`
}

// ProviderUsage is one provider's quota for one account.
type ProviderUsage struct {
	MeasuredAt time.Time     `json:"measured_at"`
	Windows    []UsageWindow `json:"windows"`
}

// ClaudeProviderUsage projects a Claude reading to its still-valid windows;
// nil when none is valid.
func ClaudeProviderUsage(r usagebudget.Reading, ok bool, now time.Time) *ProviderUsage {
	if !ok {
		return nil
	}
	five, week := usagebudget.LiveWindows(r, now)
	out := &ProviderUsage{}
	for _, c := range []struct {
		kind string
		w    *usagebudget.Window
	}{{WindowFiveHour, five}, {WindowSevenDay, week}} {
		if c.w == nil {
			continue
		}
		out.Windows = append(out.Windows, UsageWindow{Kind: c.kind, PercentUsed: c.w.Percentage, ResetsAt: c.w.ResetAt.UTC()})
		at := c.w.LastProbedAt
		if at.IsZero() {
			at = r.LastProbedAt
		}
		if at.After(out.MeasuredAt) {
			out.MeasuredAt = at.UTC()
		}
	}
	if len(out.Windows) == 0 {
		return nil
	}
	return out
}

// CodexHomeSnapshot is what an account's own Codex home reports.
type CodexHomeSnapshot struct {
	Usage *ProviderUsage
	Plan  string
}

type codexHomeRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		RateLimits *struct {
			PlanType  string           `json:"plan_type"`
			Primary   *codexRateWindow `json:"primary"`
			Secondary *codexRateWindow `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

type codexHomeCacheKey struct {
	path string
	mod  time.Time
	size int64
}

var codexHomeCache struct {
	sync.Mutex
	entries map[string]codexHomeCacheEntry
}

type codexHomeCacheEntry struct {
	key  codexHomeCacheKey
	snap CodexHomeSnapshot
	ok   bool
}

// CodexHomeUsage reads the newest rollout under <home>/sessions for the
// short and weekly quota windows. It reads only rate_limits fields (never
// auth files), tails the file, and caches by path+mtime+size. Expired windows
// are dropped; no valid window leaves Usage nil.
func CodexHomeUsage(home string, now time.Time) CodexHomeSnapshot {
	path, mod, err := latestRolloutIn(filepath.Join(home, "sessions"))
	if err != nil {
		return CodexHomeSnapshot{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return CodexHomeSnapshot{}
	}
	key := codexHomeCacheKey{path: path, mod: mod, size: info.Size()}
	codexHomeCache.Lock()
	if e, hit := codexHomeCache.entries[home]; hit && e.key == key {
		codexHomeCache.Unlock()
		return filterCodexSnapshot(e.snap, now)
	}
	codexHomeCache.Unlock()

	snap, ok := scanRolloutTail(path, mod)
	codexHomeCache.Lock()
	if codexHomeCache.entries == nil {
		codexHomeCache.entries = map[string]codexHomeCacheEntry{}
	}
	codexHomeCache.entries[home] = codexHomeCacheEntry{key: key, snap: snap, ok: ok}
	codexHomeCache.Unlock()
	if !ok {
		return CodexHomeSnapshot{}
	}
	return filterCodexSnapshot(snap, now)
}

func filterCodexSnapshot(s CodexHomeSnapshot, now time.Time) CodexHomeSnapshot {
	out := CodexHomeSnapshot{Plan: s.Plan}
	if s.Usage == nil {
		return out
	}
	u := &ProviderUsage{MeasuredAt: s.Usage.MeasuredAt}
	for _, w := range s.Usage.Windows {
		if w.ResetsAt.After(now) {
			u.Windows = append(u.Windows, w)
		}
	}
	if len(u.Windows) > 0 {
		out.Usage = u
	}
	return out
}

func latestRolloutIn(root string) (string, time.Time, error) {
	var (
		best string
		mod  time.Time
	)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !rolloutFileRe.MatchString(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(mod) {
			best, mod = p, info.ModTime()
		}
		return nil
	})
	if err == nil && best == "" {
		err = os.ErrNotExist
	}
	return best, mod, err
}

func scanRolloutTail(path string, mod time.Time) (CodexHomeSnapshot, bool) {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("[backendtier] codex home rollout open failed", "err", err)
		return CodexHomeSnapshot{}, false
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > rolloutTailBytes {
		if _, err := f.Seek(info.Size()-rolloutTailBytes, io.SeekStart); err != nil {
			return CodexHomeSnapshot{}, false
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		slog.Warn("[backendtier] codex home rollout read failed", "err", err)
		return CodexHomeSnapshot{}, false
	}
	var (
		snap  CodexHomeSnapshot
		found bool
	)
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"rate_limits"`)) {
			continue
		}
		var rec codexHomeRecord
		if json.Unmarshal(line, &rec) != nil || rec.Payload.RateLimits == nil {
			continue
		}
		rl := rec.Payload.RateLimits
		at := rec.Timestamp
		if at.IsZero() {
			at = mod
		}
		u := &ProviderUsage{MeasuredAt: at.UTC()}
		for _, w := range []*codexRateWindow{rl.Primary, rl.Secondary} {
			if w == nil {
				continue
			}
			kind := ""
			switch w.WindowMinutes {
			case codexShortWindowMinutes:
				kind = WindowFiveHour
			case codexWeeklyWindowMinutes:
				kind = WindowSevenDay
			default:
				continue
			}
			u.Windows = append(u.Windows, UsageWindow{Kind: kind, PercentUsed: w.UsedPercent, ResetsAt: time.Unix(w.ResetsAt, 0).UTC()})
		}
		next := CodexHomeSnapshot{Plan: rl.PlanType}
		if len(u.Windows) > 0 {
			next.Usage = u
		}
		snap, found = next, true
	}
	return snap, found
}
