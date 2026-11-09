package daemon

import (
	"context"
	"os"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// pollManualPRsOnce verifies each manually linked PR directly by url. A
// lookup error keeps the association and the last verified cache untouched.
func (d *Daemon) pollManualPRsOnce(ctx context.Context) {
	if d.prDisableFlagPath != "" {
		if _, err := os.Stat(d.prDisableFlagPath); err == nil {
			return
		}
	}
	links, err := d.db.ManualPRs()
	if err != nil {
		uxlog.Log("[pr] manual poll: read links failed: %v", err)
		return
	}
	if len(links) == 0 {
		return
	}
	prMeta, err := d.db.ListMetaByNamespace("pr")
	if err != nil {
		uxlog.Log("[pr] manual poll: read cached pr states failed: %v", err)
		prMeta = nil
	}
	for id, url := range links {
		if ctx.Err() != nil {
			return
		}
		task, gerr := d.db.Get(id)
		if gerr != nil || task == nil {
			uxlog.Log("[pr] manual poll: skip missing/unreadable task %s: %v", id, gerr)
			continue
		}
		if task.Archived {
			uxlog.Log("[pr] manual poll: skip archived task %s", id)
			continue
		}
		state, _ := model.ParsePRState(prMeta[id]["state"])
		cacheMatches := prMeta[id]["url"] == url
		terminalAtStart := cacheMatches && state.IsTerminal()
		lifecycle := model.ParsePRLifecycle(prMeta[id]["lifecycle"])
		if !cacheMatches {
			lifecycle = model.PRLifecycleUnknown
			// Keep a terminal marker for its bounded immediate verification attempt.
			if !state.IsTerminal() {
				state = model.PRUnknown
			}
		}
		if !d.shouldPollPR(task, state, lifecycle, time.Now()) {
			uxlog.Log("[pr] manual poll: skip task %s (state=%s, not due or verified terminal)", id, state)
			continue
		}
		res, ferr := d.prURLFetch(ctx, url)
		if ferr != nil {
			uxlog.Log("[pr] manual poll: %s lookup failed (keeping association): %v", id, ferr)
			continue
		}
		lifecycle = res.Lifecycle()
		wrote, werr := d.db.SetManualPRMetaIfChanged(id, url, map[string]string{
			"state":     res.State.String(),
			"url":       url,
			"lifecycle": string(lifecycle),
		})
		if werr != nil {
			uxlog.Log("[pr] manual poll: persist failed for %s: %v", id, werr)
			continue
		}
		if !wrote {
			continue
		}
		events.Emit(model.EventTypeTaskPRChanged, id, model.PRChangedPayload{
			URL: url, State: res.State.String(), Lifecycle: lifecycle, Source: model.PRSourceManual,
		})
		uxlog.Log("[pr] manual poll: %s changed state=%s lifecycle=%s", id, res.State, lifecycle)
		if res.Merged && !terminalAtStart {
			if t, gerr := d.db.Get(id); gerr == nil && t != nil {
				d.notifyCoordinatorOfMergedPR(t, url)
				d.notifyRoleOfMergedPR(t, url)
			}
		} else if res.Merged {
			uxlog.Log("[pr] manual poll: suppress historical merge notices for %s", id)
		}
	}
}
