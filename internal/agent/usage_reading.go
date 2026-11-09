package agent

import (
	"os"
	"sync"
	"time"

	"github.com/drn/argus/internal/codexctx"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/skills"
)

// UsageCounters are token counts; nil is unreported, never zero. InputTokens
// includes CachedInputTokens.
type UsageCounters struct {
	InputTokens, CachedInputTokens, OutputTokens *int64
}

// UsageTurn is one turn's spend.
type UsageTurn struct {
	ID         string
	StartedAt  time.Time
	MeasuredAt time.Time
	UsageCounters
}

// UsageReading carries validated session identity even before its first measurement.
type UsageReading struct {
	Provider, Source, SessionID string
	MeasuredAt                  time.Time
	SessionStartedAt            time.Time
	Totals                      UsageCounters
	Turns                       []UsageTurn
}

type usageCacheEntry struct {
	size    int64
	modTime time.Time
	usage   codexctx.SessionUsage
}

var (
	usageCacheMu sync.Mutex
	usageCache   = map[string]usageCacheEntry{}
)

// usageCacheMax bounds the parse cache; it is cleared wholesale when exceeded.
const usageCacheMax = 64

// codexUsageOf parses the opened rollout, reusing the last parse while the
// file's size and mtime are unchanged so a per-tick poll stays cheap.
func codexUsageOf(f *os.File, path string) (codexctx.SessionUsage, bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return codexctx.SessionUsage{}, false, err
	}
	usageCacheMu.Lock()
	e, hit := usageCache[path]
	usageCacheMu.Unlock()
	if hit && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) {
		return e.usage, true, nil
	}
	u, ok, err := codexctx.UsageInFile(f)
	if err != nil || !ok {
		return u, ok, err
	}
	usageCacheMu.Lock()
	if len(usageCache) >= usageCacheMax {
		usageCache = map[string]usageCacheEntry{}
	}
	usageCache[path] = usageCacheEntry{size: fi.Size(), modTime: fi.ModTime(), usage: u}
	usageCacheMu.Unlock()
	return u, true, nil
}

// readCodexUsage measures the task's recorded Codex conversation, resolved
// through its own account's home and scoped to its worktree and session.
func readCodexUsage(task *model.Task, cfg config.Config) (UsageReading, bool, error) {
	home, err := CodexHomeForTask(task, cfg)
	if err != nil {
		return UsageReading{}, false, err
	}
	stateDB, err := CodexStateDBPath(home)
	if err != nil {
		return UsageReading{}, false, err
	}
	th, ok, err := codexctx.FindThread(stateDB, task.Worktree, task.SessionID)
	if err != nil || !ok {
		return UsageReading{}, false, err
	}
	if home == "" {
		home, err = skills.UserCodexHome()
		if err != nil {
			return UsageReading{}, false, err
		}
	}
	overlay, err := skills.ArgusCodexHomeFor(home)
	if err != nil {
		return UsageReading{}, false, err
	}
	f, err := codexctx.OpenThreadRollout(home, th, overlay)
	if err != nil {
		if os.IsNotExist(err) {
			return UsageReading{}, false, nil
		}
		return UsageReading{}, false, err
	}
	defer f.Close() //nolint:errcheck // read-only handle
	started, err := codexctx.ValidateThreadRollout(f, task.Worktree, th)
	if err != nil {
		return UsageReading{}, false, err
	}
	out := UsageReading{
		Provider: ContextProviderOpenAI, Source: ContextSourceCodex, SessionID: th.ID,
		SessionStartedAt: started,
	}
	u, ok, err := codexUsageOf(f, th.RolloutPath)
	if err != nil || !ok {
		return out, false, err
	}
	out.MeasuredAt, out.Totals = u.MeasuredAt, UsageCounters(u.Totals)
	for _, t := range u.Turns {
		out.Turns = append(out.Turns, UsageTurn{ID: t.ID, StartedAt: t.StartedAt, MeasuredAt: t.MeasuredAt, UsageCounters: UsageCounters(t.Counters)})
	}
	return out, true, nil
}
