package backendtier

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func writeRollout(t *testing.T, home, name string, mod time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "11", "07")
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	testutil.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	testutil.NoError(t, os.Chtimes(p, mod, mod))
	return p
}

func rateLine(ts string, plan string, shortPct, weekPct float64, shortReset, weekReset int64) string {
	return fmt.Sprintf(`{"timestamp":%q,"payload":{"rate_limits":{"plan_type":%q,"primary":{"used_percent":%g,"window_minutes":300,"resets_at":%d},"secondary":{"used_percent":%g,"window_minutes":10080,"resets_at":%d}}}}`,
		ts, plan, shortPct, shortReset, weekPct, weekReset)
}

func TestCodexHomeUsage(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Hour).Unix()
	weekFuture := now.Add(72 * time.Hour).Unix()

	t.Run("short and weekly windows with plan", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now, rateLine("2026-11-07T11:59:00Z", "plus", 25, 50, future, weekFuture))
		got := CodexHomeUsage(home, now)
		testutil.Equal(t, got.Plan, "plus")
		testutil.True(t, got.Usage != nil)
		testutil.Equal(t, len(got.Usage.Windows), 2)
		testutil.Equal(t, got.Usage.Windows[0].Kind, WindowFiveHour)
		testutil.Equal(t, got.Usage.Windows[0].PercentUsed, 25.0)
		testutil.Equal(t, got.Usage.Windows[1].Kind, WindowSevenDay)
		testutil.Equal(t, got.Usage.Windows[1].ResetsAt.Unix(), weekFuture)
		testutil.Equal(t, got.Usage.MeasuredAt, time.Date(2026, 11, 7, 11, 59, 0, 0, time.UTC))
	})

	t.Run("measured zero is distinct from missing", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now, rateLine("2026-11-07T11:59:00Z", "", 0, 0, future, weekFuture))
		got := CodexHomeUsage(home, now)
		testutil.True(t, got.Usage != nil)
		testutil.Equal(t, got.Usage.Windows[0].PercentUsed, 0.0)
		testutil.True(t, CodexHomeUsage(t.TempDir(), now).Usage == nil)
	})

	t.Run("expired window is omitted independently", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now, rateLine("2026-11-07T08:00:00Z", "", 90, 40, now.Add(-time.Hour).Unix(), weekFuture))
		got := CodexHomeUsage(home, now)
		testutil.Equal(t, len(got.Usage.Windows), 1)
		testutil.Equal(t, got.Usage.Windows[0].Kind, WindowSevenDay)
	})

	t.Run("all windows expired leaves usage nil", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now, rateLine("2026-11-01T08:00:00Z", "pro", 90, 40, now.Add(-2*time.Hour).Unix(), now.Add(-time.Hour).Unix()))
		got := CodexHomeUsage(home, now)
		testutil.True(t, got.Usage == nil)
		testutil.Equal(t, got.Plan, "pro")
	})

	t.Run("depleted record with null windows is unavailable", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now,
			rateLine("2026-11-07T10:00:00Z", "", 10, 10, future, weekFuture),
			`{"timestamp":"2026-11-07T11:00:00Z","payload":{"rate_limits":{"primary":null,"secondary":null,"rate_limit_reached_type":"x"}}}`)
		testutil.True(t, CodexHomeUsage(home, now).Usage == nil)
	})

	t.Run("homes are isolated", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		writeRollout(t, a, "rollout-a.jsonl", now, rateLine("2026-11-07T11:00:00Z", "", 11, 22, future, weekFuture))
		testutil.Equal(t, CodexHomeUsage(a, now).Usage.Windows[0].PercentUsed, 11.0)
		testutil.True(t, CodexHomeUsage(b, now).Usage == nil)
	})

	t.Run("newer rollout replaces and cache refreshes on append", func(t *testing.T) {
		home := t.TempDir()
		p := writeRollout(t, home, "rollout-a.jsonl", now.Add(-time.Hour), rateLine("2026-11-07T10:00:00Z", "", 10, 20, future, weekFuture))
		testutil.Equal(t, CodexHomeUsage(home, now).Usage.Windows[0].PercentUsed, 10.0)
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
		testutil.NoError(t, err)
		_, err = f.WriteString(rateLine("2026-11-07T11:30:00Z", "", 60, 70, future, weekFuture) + "\n")
		testutil.NoError(t, err)
		testutil.NoError(t, f.Close())
		testutil.Equal(t, CodexHomeUsage(home, now).Usage.Windows[0].PercentUsed, 60.0)
		writeRollout(t, home, "rollout-b.jsonl", time.Now().Add(time.Hour), rateLine("2026-11-07T11:45:00Z", "", 5, 6, future, weekFuture))
		testutil.Equal(t, CodexHomeUsage(home, now).Usage.Windows[0].PercentUsed, 5.0)
	})

	t.Run("unrecognised window length is ignored", func(t *testing.T) {
		home := t.TempDir()
		writeRollout(t, home, "rollout-a.jsonl", now, fmt.Sprintf(`{"timestamp":"2026-11-07T11:00:00Z","payload":{"rate_limits":{"primary":{"used_percent":5,"window_minutes":60,"resets_at":%d}}}}`, future))
		testutil.True(t, CodexHomeUsage(home, now).Usage == nil)
	})
}

func TestClaudeProviderUsage(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	probed := now.Add(-time.Minute)
	t.Run("no reading is nil", func(t *testing.T) {
		testutil.True(t, ClaudeProviderUsage(usagebudget.Reading{}, false, now) == nil)
	})
	t.Run("expired window dropped, valid kept", func(t *testing.T) {
		r := usagebudget.Reading{
			FiveHour: &usagebudget.Window{Percentage: 0, ResetAt: now.Add(time.Hour), LastProbedAt: probed},
			SevenDay: &usagebudget.Window{Percentage: 50, ResetAt: now.Add(-time.Hour), LastProbedAt: probed},
		}
		got := ClaudeProviderUsage(r, true, now)
		testutil.True(t, got != nil)
		testutil.Equal(t, len(got.Windows), 1)
		testutil.Equal(t, got.Windows[0].Kind, WindowFiveHour)
		testutil.Equal(t, got.Windows[0].PercentUsed, 0.0)
		testutil.Equal(t, got.MeasuredAt, probed)
	})
	t.Run("nothing valid is nil", func(t *testing.T) {
		r := usagebudget.Reading{FiveHour: &usagebudget.Window{ResetAt: now.Add(-time.Hour), LastProbedAt: probed}}
		testutil.True(t, ClaudeProviderUsage(r, true, now) == nil)
	})
}
