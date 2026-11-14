package heragater

import (
	"bytes"
	"log/slog"
	"os/exec"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestGater_TargetProjectBase(t *testing.T) {
	for _, tc := range []struct {
		name, source, explicit, want string
		blocker, subcoord, missing   bool
	}{
		{name: "foreign root", source: "other"},
		{name: "foreign blocker with matching ref", source: "other", blocker: true},
		{name: "foreign subcoord", source: "other", subcoord: true},
		{name: "foreign blocker subcoord", source: "other", blocker: true, subcoord: true},
		{name: "same project root", source: "proj", want: "stack"},
		{name: "same project blocker", source: "proj", blocker: true, want: "stack"},
		{name: "same project subcoord", source: "proj", subcoord: true, want: "stack"},
		{name: "missing explicit base", source: "proj", explicit: "missing"},
		{name: "missing coordinator base", source: "proj", missing: true},
		{name: "missing blocker base", source: "proj", blocker: true, missing: true},
		{name: "valid explicit base across projects", source: "other", explicit: "stack", want: "stack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGaterFixture(t)
			repo := f.d.Config().Projects["proj"].Path
			orch := f.seedCoordWithBranch(t, "orch", tc.explicit, "stack")
			if tc.explicit == "missing" {
				cmd := exec.Command("git", "branch", "-D", "missing")
				cmd.Dir = repo
				testutil.NoError(t, cmd.Run())
			}
			coords, err := f.d.ListHeraRolesByKind(orch, db.HeraKindCoordinator)
			testutil.NoError(t, err)
			sourceID := coords[0].ID
			if tc.blocker {
				sourceID = f.boundWorker(t, orch, "upstream", model.StatusInReview, db.HeraStatusDone).ID
			}
			binding, err := f.d.HeraLiveBindingByRole(sourceID)
			testutil.NoError(t, err)
			task, err := f.d.Get(binding.ArgusTaskID)
			testutil.NoError(t, err)
			task.Project, task.Branch = tc.source, "stack"
			if tc.missing {
				task.Branch = "missing"
			}
			testutil.NoError(t, f.d.Update(task))
			node := f.planned(t, orch, "next")
			if tc.blocker {
				testutil.NoError(t, f.d.AddHeraBlock(node.ID, sourceID))
			}
			if tc.subcoord {
				node.NodeKind = db.HeraNodeKindSubCoord
				f.w.SetSubCoordMaterializer(f.w.materialize)
			}
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			f.w.materializeNode(node)
			testutil.Equal(t, len(f.materialized()), 1)
			testutil.Equal(t, f.matBranch[node.ID], tc.want)
			if tc.want == "" {
				testutil.Contains(t, logs.String(), "not in project proj; using project default")
			}
		})
	}
}
