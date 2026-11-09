package tiering

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResolve_BoundTaskEffort(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	testutil.NoError(t, d.Add(&model.Task{ID: "moved", Name: "moved", Backend: "claude", Archetype: "code_slice"}))
	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{TaskID: "moved", Kind: db.HeraKindWorker}
	got := r.Resolve(&rv)
	t.Logf("resolution after hera_move = %+v", got)
	testutil.Equal(t, got.EffectiveModel, "sonnet")
	testutil.Equal(t, got.Effort, "high")
	r.Annotate(&rv)
	testutil.Equal(t, rv.AppliedModel, "")
	testutil.Equal(t, rv.AppliedEffort, "")
}

func TestResolve_InvalidPanelWarning(t *testing.T) {
	d := setup(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Profile: "broken-panel"}))
	writeProfile(t, "broken-panel", leanProfile+"[panel]\nfinders = []\n")
	testutil.NoError(t, d.Add(&model.Task{ID: "panel", Name: "panel", Project: "p", Backend: "claude", Archetype: "code_slice"}))
	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{TaskID: "panel", ArgusProject: "p", Archetype: "code_slice"}
	got := r.Resolve(&rv)
	t.Logf("resolution with invalid panel = %+v", got)
	testutil.Equal(t, got.Effort, "")
	testutil.Equal(t, got.Source, SourceCLIDefault)
	testutil.Equal(t, got.Warning, `profile "broken-panel" missing or invalid`)
}

func TestAnnotate_OverrideWithInvalidProfile(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	testutil.NoError(t, d.Add(&model.Task{ID: "override", Name: "override", Backend: "claude", Archetype: "code_slice", Model: "opus", Profile: "missing"}))
	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{TaskID: "override", Archetype: "code_slice"}
	r.Annotate(&rv)
	t.Logf("TUI applied_model=%q warning=%q", rv.AppliedModel, rv.ProfileWarning)
	testutil.Equal(t, rv.AppliedModel, "opus")
	testutil.Equal(t, rv.ProfileWarning, `profile "missing" missing or invalid`)
}

func TestResolve_MissingBackendWithProfile(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	testutil.NoError(t, d.DeleteBackend("claude"))
	testutil.NoError(t, d.Add(&model.Task{ID: "missing", Name: "missing", Backend: "claude", Archetype: "code_slice"}))
	rv := heramodel.RoleView{TaskID: "missing", Archetype: "code_slice"}
	r := NewResolver(DefaultOptions(d))
	got := r.Resolve(&rv)
	testutil.Equal(t, got.Source, SourceUnavailable)
	testutil.Equal(t, got.EffectiveModel, "")
	testutil.Equal(t, got.Warning, "backend could not be resolved")
	r.Annotate(&rv)
	testutil.Equal(t, rv.AppliedModel, "sonnet")
	testutil.Equal(t, rv.AppliedEffort, "high")
}

func TestResolver_ProfileSnapshot(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	loads := 0
	opts := DefaultOptions(d)
	opts.Validate = func(l *profiles.Loader, name string, cfg config.Config) (*profiles.Profile, []error) {
		loads++
		p, errs := DefaultValidate(l, name, cfg)
		writeProfile(t, name, "not = [valid")
		return p, errs
	}
	r := NewResolver(opts)
	for _, project := range []string{"a", "b"} {
		t.Run(project, func(t *testing.T) {
			rv := heramodel.RoleView{ArgusProject: project, Archetype: "code_slice"}
			got := r.Resolve(&rv)
			testutil.Equal(t, got.EffectiveModel, "sonnet")
			testutil.Equal(t, got.Effort, "high")
		})
	}
	testutil.Equal(t, loads, 1)
	testutil.Equal(t, r.modelMiss, 2)
}

func TestResolver_ModelMemo(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	r := NewResolver(DefaultOptions(d))
	for i := 0; i < 20; i++ {
		rv := heramodel.RoleView{Archetype: "code_slice"}
		testutil.Equal(t, r.Resolve(&rv).EffectiveModel, "sonnet")
	}
	testutil.Equal(t, r.modelMiss, 1)
	testutil.Equal(t, r.modelHits, 19)
}
