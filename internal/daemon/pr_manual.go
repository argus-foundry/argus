package daemon

import (
	"context"
	"os"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
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
		if st, perr := model.ParsePRState(prMeta[id]["state"]); perr == nil && st.IsTerminal() &&
			model.ParsePRLifecycle(prMeta[id]["lifecycle"]) != model.PRLifecycleUnknown {
			continue
		}
		res, ferr := d.prURLFetch(ctx, url)
		if ferr != nil {
			uxlog.Log("[pr] manual poll: %s lookup failed (keeping association): %v", id, ferr)
			continue
		}
		lifecycle := res.Lifecycle()
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
		if res.Merged {
			if t, gerr := d.db.Get(id); gerr == nil && t != nil {
				d.notifyCoordinatorOfMergedPR(t, url)
				d.notifyRoleOfMergedPR(t, url)
			}
		}
	}
}
