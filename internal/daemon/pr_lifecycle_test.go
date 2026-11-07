package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type prEventSink struct {
	mu  sync.Mutex
	got []model.Event
}

func (s *prEventSink) Emit(ev model.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, ev)
}

func (s *prEventSink) prChanged() []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Event
	for _, ev := range s.got {
		if ev.Type == model.EventTypeTaskPRChanged {
			out = append(out, ev)
		}
	}
	return out
}

func installPRSink(t *testing.T) *prEventSink {
	t.Helper()
	sink := &prEventSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	return sink
}

func storedLifecycle(t *testing.T, d *Daemon, id string) string {
	t.Helper()
	entries, err := d.db.ListMeta(id, "pr")
	testutil.NoError(t, err)
	for _, e := range entries {
		if e.Key == "lifecycle" {
			return e.Value
		}
	}
	return ""
}

func TestPollPR_PersistsLifecycleAndEmitsEvent(t *testing.T) {
	cases := []struct {
		name      string
		res       gitutil.PRResult
		lifecycle string
	}{
		{"merged", gitutil.PRResult{State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/7", Merged: true}, "merged"},
		{"closed without merge", gitutil.PRResult{State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/7"}, "closed"},
		{"open", gitutil.PRResult{State: model.PRApproved, URL: "https://github.com/o/r/pull/7"}, "open"},
		{"no pr", gitutil.PRResult{State: model.PRNone}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := installPRSink(t)
			d, _ := testDaemon(t)
			seedTask(t, d.db, "t1", "argus/t1", false)
			d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
				out := map[string]gitutil.PRResult{}
				for b := range branches {
					out[b] = tc.res
				}
				return out, 1, nil
			}
			d.prResolveRepo = resolveAll("o/r")

			d.pollPRStatesOnce(context.Background())

			testutil.Equal(t, storedLifecycle(t, d, "t1"), tc.lifecycle)
			evs := sink.prChanged()
			testutil.Equal(t, len(evs), 1)
			testutil.Equal(t, evs[0].TaskID, "t1")
			var payload map[string]string
			testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
			testutil.DeepEqual(t, payload, map[string]string{
				"url": tc.res.URL, "state": tc.res.State.String(), "lifecycle": tc.lifecycle, "source": "automatic",
			})

			d.pollPRStatesOnce(context.Background())
			testutil.Equal(t, len(sink.prChanged()), 1)
		})
	}
}

func TestPollPR_TerminalWithoutLifecycleIsRefreshedThenSkipped(t *testing.T) {
	sink := installPRSink(t)
	d, _ := testDaemon(t)
	seedTask(t, d.db, "old", "argus/old", false)
	testutil.NoError(t, d.db.SetMetaBatch("old", "pr", map[string]string{
		"state": model.PRMergedClosed.String(), "url": "https://github.com/o/r/pull/3",
	}))
	calls := 0
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		calls++
		out := map[string]gitutil.PRResult{}
		for b := range branches {
			out[b] = gitutil.PRResult{State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/3"}
		}
		return out, 1, nil
	}
	d.prResolveRepo = resolveAll("o/r")

	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, calls, 1)
	testutil.Equal(t, storedLifecycle(t, d, "old"), "closed")
	testutil.Equal(t, len(sink.prChanged()), 1)

	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, calls, 1)
}

func TestPollPR_FetchFailureKeepsLastVerifiedLifecycle(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "k", "argus/k", false)
	testutil.NoError(t, d.db.SetMetaBatch("k", "pr", map[string]string{
		"state": model.PRApproved.String(), "url": "https://github.com/o/r/pull/4", "lifecycle": "open",
	}))
	sink := installPRSink(t)
	d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
		return nil, 0, context.DeadlineExceeded
	}
	d.prResolveRepo = resolveAll("o/r")

	d.pollPRStatesOnce(context.Background())

	testutil.Equal(t, storedLifecycle(t, d, "k"), "open")
	testutil.Equal(t, len(sink.prChanged()), 0)
}

func TestPollPR_ClosedPRDoesNotNotifyMerge(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "c", "argus/c", false)
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		out := map[string]gitutil.PRResult{}
		for b := range branches {
			out[b] = gitutil.PRResult{State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/5"}
		}
		return out, 1, nil
	}
	d.prResolveRepo = resolveAll("o/r")
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, storedLifecycle(t, d, "c"), "closed")
	testutil.Equal(t, gitutil.PRResult{State: model.PRMergedClosed}.Lifecycle(), model.PRLifecycleClosed)
}
