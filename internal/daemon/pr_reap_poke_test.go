package daemon

import (
	"context"
	"testing"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// add-coord-reap-triggers: a committed PR-state change pokes the reap nudge
// watcher once; an unchanged re-poll commits nothing and does not poke.
func TestPollPR_StateChangePokesReapNudger(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "t1", "argus/t1", false)
	pokes := 0
	d.heraReapPoke = func() { pokes++ }
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		out := map[string]gitutil.PRResult{}
		for branch := range branches {
			out[branch] = gitutil.PRResult{State: model.PRApproved, URL: "https://example/pr/9"}
		}
		return out, 1, nil
	}
	d.prResolveRepo = resolveAll("drn/argus")
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, pokes, 1)
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, pokes, 1)
}
