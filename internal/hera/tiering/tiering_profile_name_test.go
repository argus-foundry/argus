package tiering

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResolve_ProjectProfileWhitespaceMatchesLaunch(t *testing.T) {
	for _, tc := range []struct{ name, binding, backendModel, want, source string }{
		{"padded name", " lean ", "", "", SourceCLIDefault},
		{"whitespace only", "  ", "", "", SourceCLIDefault},
		{"backend fallback", " lean ", "haiku", "haiku", SourceBackendDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := setup(t)
			writeProfile(t, "lean", leanProfile)
			writeProfile(t, "default", leanProfile)
			testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude", Model: tc.backendModel}))
			testutil.NoError(t, d.SetProject("p", config.Project{Profile: tc.binding}))
			task := &model.Task{ID: "profile-space", Name: "profile-space", Project: "p", Backend: "claude", Archetype: "code_slice"}
			testutil.NoError(t, d.Add(task))
			cfg := d.Config()
			backend, err := agent.ResolveBackend(task, cfg)
			testutil.NoError(t, err)
			launch, profile := agent.ResolveModel(task, backend, cfg)
			testutil.Equal(t, launch, tc.want)
			testutil.Nil(t, profile)
			rv := heramodel.RoleView{TaskID: task.ID, ArgusProject: "p", Archetype: "code_slice"}
			got := NewResolver(DefaultOptions(d)).Resolve(&rv)
			testutil.Equal(t, got.EffectiveModel, launch)
			testutil.Equal(t, got.Source, tc.source)
			testutil.Equal(t, got.Profile, tc.binding)
			testutil.Equal(t, got.Effort, "")
			testutil.Contains(t, got.Warning, "missing or invalid")
		})
	}
}
