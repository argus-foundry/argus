package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

type usageSink struct {
	mu  sync.Mutex
	evs []model.Event
}

func (s *usageSink) Emit(ev model.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, ev)
}

func (s *usageSink) usage() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]string
	for _, e := range s.evs {
		if e.Type != model.EventTypeAccountUsageChanged {
			continue
		}
		m := map[string]string{}
		_ = json.Unmarshal(e.Payload, &m)
		out = append(out, m)
	}
	return out
}

func TestUsageWatcher_EmitsOnChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sink := &usageSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })

	home := t.TempDir()
	cfg := config.Config{Accounts: map[string]config.Account{"work": {CodexHome: home}}}
	now := time.Now()
	write := func(pct float64) {
		dir := filepath.Join(home, "sessions", "2026", "11", "07")
		testutil.NoError(t, os.MkdirAll(dir, 0o755))
		line := fmt.Sprintf(`{"timestamp":%q,"payload":{"rate_limits":{"primary":{"used_percent":%g,"window_minutes":300,"resets_at":%d}}}}`,
			now.UTC().Format(time.RFC3339), pct, now.Add(time.Hour).Unix())
		p := filepath.Join(dir, "rollout-x.jsonl")
		testutil.NoError(t, os.WriteFile(p, []byte(line+"\n"), 0o644))
		testutil.NoError(t, os.Chtimes(p, now.Add(time.Duration(pct)*time.Second), now.Add(time.Duration(pct)*time.Second)))
	}

	w := &usageWatcher{}
	write(10)
	w.tick(cfg, now)
	testutil.Equal(t, len(sink.usage()), 0)

	w.tick(cfg, now)
	testutil.Equal(t, len(sink.usage()), 0)

	write(30)
	w.tick(cfg, now)
	got := sink.usage()
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, got[0]["account"], "work")
	testutil.Equal(t, got[0]["kind"], "codex")
	testutil.Equal(t, got[0]["measured_at"], now.UTC().Format(time.RFC3339))
	_, hasPct := got[0]["percent_used"]
	testutil.False(t, hasPct)

	w.tick(cfg, now.Add(2*time.Hour))
	got = sink.usage()
	testutil.Equal(t, len(got), 2)
	_, hasAt := got[1]["measured_at"]
	testutil.False(t, hasAt)
}

func (s *usageSink) tiers() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]string
	for _, e := range s.evs {
		if e.Type != model.EventTypeBackendTiersUsageChanged {
			continue
		}
		m := map[string]string{}
		_ = json.Unmarshal(e.Payload, &m)
		out = append(out, m)
	}
	return out
}

func TestUsageWatcher_BackendTierReadings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sink := &usageSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })

	cfg := config.Config{Backends: map[string]config.Backend{"claude": {Command: "claude"}}}
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}}
	now := time.Now()
	reading := func(pct float64) map[string]usagebudget.Reading {
		return map[string]usagebudget.Reading{config.DefaultAccountName: {Percentage: pct, ResetAt: now.Add(time.Hour)}}
	}
	w := &usageWatcher{}

	w.tickTiers(cfg, reading(10), now)
	testutil.Equal(t, len(sink.tiers()), 0)
	w.tickTiers(cfg, reading(10), now)
	testutil.Equal(t, len(sink.tiers()), 0)

	w.tickTiers(cfg, reading(20), now)
	got := sink.tiers()
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, got[0]["revision"], backendtier.TierRevision(cfg.BackendRouting.Tiers))
	testutil.Equal(t, len(got[0]), 1)

	w.tickTiers(cfg, map[string]usagebudget.Reading{}, now)
	testutil.Equal(t, len(sink.tiers()), 2)
}
