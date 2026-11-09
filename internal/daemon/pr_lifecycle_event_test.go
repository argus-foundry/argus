package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type prCommitSink struct {
	t     *testing.T
	d     *Daemon
	count int
}

func (s *prCommitSink) Emit(ev model.Event) {
	if ev.Type != model.EventTypeTaskPRChanged {
		return
	}
	s.count++
	done := make(chan map[string]string, 1)
	go func() {
		entries, err := s.d.db.ListMeta(ev.TaskID, "pr")
		got := map[string]string{}
		if err != nil {
			got["error"] = err.Error()
		}
		for _, entry := range entries {
			got[entry.Key] = entry.Value
		}
		done <- got
	}()
	select {
	case got := <-done:
		var payload map[string]string
		testutil.NoError(s.t, json.Unmarshal(ev.Payload, &payload))
		testutil.Equal(s.t, payload["source"], "automatic")
		delete(payload, "source")
		testutil.DeepEqual(s.t, got, payload)
	case <-time.After(time.Second):
		s.t.Error("PR event sink cannot read committed cache during callback")
	}
}

func TestPollPR_CallbackReadsCommittedCache(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "qa", "argus/qa", false)
	sink := &prCommitSink{t: t, d: d}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	d.prResolveRepo = resolveAll("o/r")
	d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
		return map[string]gitutil.PRResult{"argus/qa": {State: model.PRApproved, URL: "https://github.com/o/r/pull/7"}}, 1, nil
	}
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, sink.count, 1)
}

func TestPollPR_VerifiedCacheChangeEmits(t *testing.T) {
	for _, change := range []string{"state", "url", "merged", "closed"} {
		t.Run(change, func(t *testing.T) {
			d, _ := testDaemon(t)
			seedTask(t, d.db, "qa", "argus/qa", false)
			url := "https://github.com/o/r/pull/7"
			testutil.NoError(t, d.db.SetMetaBatch("qa", "pr", map[string]string{"state": "approved", "lifecycle": "open", "url": url}))
			res := gitutil.PRResult{State: model.PRApproved, URL: url}
			switch change {
			case "state":
				res.State = model.PRChangesRequested
			case "url":
				res.URL = "https://github.com/o/r/pull/8"
			case "closed":
				res.State = model.PRMergedClosed
			case "merged":
				res.State = model.PRMergedClosed
				res.Merged = true
			}
			sink := installPRSink(t)
			d.prResolveRepo = resolveAll("o/r")
			d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
				return map[string]gitutil.PRResult{"argus/qa": res}, 1, nil
			}
			d.pollPRStatesOnce(context.Background())
			evs := sink.prChanged()
			if len(evs) != 1 {
				t.Fatalf("got %d PR events, want 1", len(evs))
			}
			var payload map[string]string
			testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
			want := map[string]string{"state": res.State.String(), "url": res.URL, "lifecycle": string(res.Lifecycle())}
			testutil.Equal(t, payload["source"], "automatic")
			delete(payload, "source")
			testutil.DeepEqual(t, payload, want)
			entries, err := d.db.ListMeta("qa", "pr")
			testutil.NoError(t, err)
			got := map[string]string{}
			for _, e := range entries {
				got[e.Key] = e.Value
			}
			testutil.DeepEqual(t, got, want)
			d.pollPRStatesOnce(context.Background())
			testutil.Equal(t, len(sink.prChanged()), 1)
		})
	}
}

func TestPollPR_FailedWritePublishesNoEvent(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "qa", "argus/qa", false)
	sink := installPRSink(t)
	d.prResolveRepo = resolveAll("o/r")
	d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
		testutil.NoError(t, d.db.Close())
		return map[string]gitutil.PRResult{"argus/qa": {State: model.PRApproved, URL: "https://github.com/o/r/pull/7"}}, 1, nil
	}
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, len(sink.prChanged()), 0)
}
