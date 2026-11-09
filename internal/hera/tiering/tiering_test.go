package tiering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/testutil"
)

const leanProfile = "[archetype.code_slice]\nmodels = { claude = \"sonnet\" }\neffort = \"high\"\n"

func setup(t *testing.T) *db.DB {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.SetConfigValue("defaults.backend", "claude"))
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude"}))
	return d
}

func writeProfile(t *testing.T, name, content string) {
	t.Helper()
	dir := filepath.Join(db.DataDir(), "profiles")
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, name+".toml"), []byte(content), 0o644))
}

func TestResolve(t *testing.T) {
	d := setup(t)
	testutil.NoError(t, d.SetProject("lean", config.Project{Path: t.TempDir(), Profile: "lean"}))
	testutil.NoError(t, d.SetProject("ghost", config.Project{Path: t.TempDir(), Profile: "ghost"}))
	testutil.NoError(t, d.SetProject("plain", config.Project{Path: t.TempDir()}))
	writeProfile(t, "lean", leanProfile)
	writeProfile(t, "opusy", "[archetype.code_slice]\nmodels = { claude = \"opus\" }\neffort = \"low\"\n")
	writeProfile(t, "broken", "not = [valid")
	testutil.NoError(t, d.Add(&model.Task{ID: "t-ovr", Name: "o", Project: "lean", Model: "opus", Archetype: "code_slice"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t-prof", Name: "p", Project: "lean", Profile: "opusy", Archetype: "code_slice"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t-bad", Name: "b", Project: "lean", Profile: "broken", Archetype: "code_slice"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t-badmodel", Name: "bm", Project: "lean", Model: "gpt-nope", Archetype: "code_slice"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t-cli", Name: "c", Project: "plain"}))

	tests := []struct {
		name string
		rv   heramodel.RoleView
		want Resolution
	}{
		{"explicit model override", heramodel.RoleView{TaskID: "t-ovr", ArgusProject: "lean", Archetype: "code_slice"},
			Resolution{RequestedModel: "opus", EffectiveModel: "opus", Effort: "high", Source: SourceTaskOverride, Basis: BasisCurrentConfig, Profile: "lean"}},
		{"task profile override", heramodel.RoleView{TaskID: "t-prof", ArgusProject: "lean", Archetype: "code_slice"},
			Resolution{EffectiveModel: "opus", Effort: "low", Source: SourceProfile, Basis: BasisCurrentConfig, Profile: "opusy"}},
		{"project profile planned intent", heramodel.RoleView{ArgusProject: "lean", Archetype: "code_slice"},
			Resolution{EffectiveModel: "sonnet", Effort: "high", Source: SourceProfile, Basis: BasisPlannedIntent, Profile: "lean"}},
		{"invalid explicit task profile warns", heramodel.RoleView{TaskID: "t-bad", ArgusProject: "lean", Archetype: "code_slice"},
			Resolution{Source: SourceCLIDefault, Basis: BasisCurrentConfig, Profile: "broken", Warning: `profile "broken" missing or invalid`}},
		{"missing project profile warns", heramodel.RoleView{ArgusProject: "ghost", Archetype: "code_slice"},
			Resolution{Source: SourceCLIDefault, Basis: BasisPlannedIntent, Profile: "ghost", Warning: `profile "ghost" missing or invalid`}},
		{"invalid override falls through to profile", heramodel.RoleView{TaskID: "t-badmodel", ArgusProject: "lean", Archetype: "code_slice"},
			Resolution{RequestedModel: "gpt-nope", EffectiveModel: "sonnet", Effort: "high", Source: SourceProfile, Basis: BasisCurrentConfig, Profile: "lean"}},
		{"no archetype is CLI default", heramodel.RoleView{TaskID: "t-cli", ArgusProject: "plain"},
			Resolution{Source: SourceCLIDefault, Basis: BasisCurrentConfig, Profile: "default"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResolver(DefaultOptions(d))
			rv := tc.rv
			testutil.DeepEqual(t, r.Resolve(&rv), tc.want)
		})
	}
}

func TestResolve_BackendDefaultAndUnavailable(t *testing.T) {
	d := setup(t)
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude", Model: "haiku"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t1", Name: "x", Project: "p"}))
	testutil.NoError(t, d.Add(&model.Task{ID: "t2", Name: "y", Project: "p", Backend: "nope"}))

	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{TaskID: "t1", ArgusProject: "p"}
	got := r.Resolve(&rv)
	testutil.Equal(t, got.Source, SourceBackendDefault)
	testutil.Equal(t, got.EffectiveModel, "haiku")

	rv2 := heramodel.RoleView{TaskID: "t2", ArgusProject: "p"}
	got = r.Resolve(&rv2)
	testutil.Equal(t, got.Source, SourceUnavailable)
	testutil.Equal(t, got.EffectiveModel, "")
	testutil.Equal(t, got.Warning, "backend could not be resolved")
}

func TestResolve_MissingTaskFallsBackToPlanned(t *testing.T) {
	d := setup(t)
	writeProfile(t, "default", leanProfile)
	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{TaskID: "gone", Archetype: "code_slice"}
	got := r.Resolve(&rv)
	testutil.Equal(t, got.EffectiveModel, "sonnet")
	testutil.Equal(t, got.Source, SourceProfile)
}

func TestResolver_OneProfileLoadPerScope(t *testing.T) {
	d := setup(t)
	testutil.NoError(t, d.SetProject("a", config.Project{Path: t.TempDir(), Profile: "lean"}))
	testutil.NoError(t, d.SetProject("b", config.Project{Path: t.TempDir(), Profile: "opusy"}))
	writeProfile(t, "lean", leanProfile)
	writeProfile(t, "opusy", "[archetype.code_slice]\nmodels = { claude = \"opus\" }\n")
	loads := map[string]int{}
	opts := DefaultOptions(d)
	opts.Validate = func(l *profiles.Loader, name string, cfg config.Config) (*profiles.Profile, []error) {
		loads[name]++
		return DefaultValidate(l, name, cfg)
	}
	r := NewResolver(opts)
	for i := 0; i < 20; i++ {
		for _, proj := range []string{"a", "b"} {
			rv := heramodel.RoleView{ArgusProject: proj, Archetype: "code_slice"}
			r.Resolve(&rv)
			rv2 := heramodel.RoleView{ArgusProject: proj, Archetype: "code_slice"}
			r.Annotate(&rv2)
		}
	}
	testutil.Equal(t, loads["lean"], 1)
	testutil.Equal(t, loads["opusy"], 1)
	testutil.Equal(t, r.modelMiss, 2)
	testutil.Equal(t, r.modelHits, 78)
	r.Done()
}

func TestAnnotate_StampsTieringAndContext(t *testing.T) {
	d := setup(t)
	testutil.NoError(t, d.SetProject("p", config.Project{Path: t.TempDir(), Profile: "lean"}))
	writeProfile(t, "lean", leanProfile)
	r := NewResolver(DefaultOptions(d))
	rv := heramodel.RoleView{ArgusProject: "p", Archetype: "code_slice", Kind: db.HeraKindWorker, ContextSize: 400000}
	r.Annotate(&rv)
	testutil.Equal(t, rv.AppliedModel, "sonnet")
	testutil.Equal(t, rv.AppliedEffort, "high")
	testutil.Equal(t, rv.ContextPercent, 40)
	r.Annotate(nil)

	ghost := heramodel.RoleView{ArgusProject: "p", Archetype: "", Kind: db.HeraKindCoordinator, ContextSize: 150000}
	r.Annotate(&ghost)
	testutil.Equal(t, ghost.AppliedModel, "")
	testutil.Equal(t, ghost.ContextPercent, 50)
}

func TestContextPercent(t *testing.T) {
	tests := []struct{ size, budget, want int }{
		{0, 100, 0}, {-5, 100, 0}, {50, 0, 0}, {50, -1, 0}, {50, 100, 50}, {150, 100, 100}, {1, 3, 33},
	}
	for _, tc := range tests {
		testutil.Equal(t, ContextPercent(tc.size, tc.budget), tc.want)
	}
}

func TestContextLevel(t *testing.T) {
	tests := []struct{ pct, want int }{
		{0, ContextLevelNone}, {39, ContextLevelNone}, {40, ContextLevelWarm}, {64, ContextLevelWarm},
		{65, ContextLevelHot}, {89, ContextLevelHot}, {90, ContextLevelCritical}, {100, ContextLevelCritical},
	}
	for _, tc := range tests {
		testutil.Equal(t, ContextLevel(tc.pct), tc.want)
	}
}

func TestContextFor(t *testing.T) {
	cfg := config.Config{}
	cfg.Hera.CoordinatorContextBudget = 200000
	cfg.Hera.WorkerContextWindow = 1000000

	testutil.Nil(t, ContextFor(nil, cfg))
	testutil.Nil(t, ContextFor(&heramodel.RoleView{Kind: db.HeraKindWorker}, cfg))
	testutil.Nil(t, ContextFor(&heramodel.RoleView{Kind: db.HeraKindWorker, ContextSize: -3}, cfg))

	w := ContextFor(&heramodel.RoleView{Kind: db.HeraKindFreelance, ContextSize: 120000}, cfg)
	testutil.Equal(t, w.DenominatorKind, DenomWorkerWindow)
	testutil.Equal(t, *w.DenominatorTokens, 1000000)
	testutil.Equal(t, *w.Percent, 12)

	c := ContextFor(&heramodel.RoleView{Kind: db.HeraKindCoordinator, ContextSize: 100000}, cfg)
	testutil.Equal(t, c.DenominatorKind, DenomCoordRecycle)
	testutil.Equal(t, *c.DenominatorTokens, 200000)
	testutil.Equal(t, *c.Percent, 50)

	over := ContextFor(&heramodel.RoleView{Kind: db.HeraKindWorker, ContextSize: 5000000}, cfg)
	testutil.Equal(t, *over.Percent, 100)

	noBudget := ContextFor(&heramodel.RoleView{Kind: db.HeraKindWorker, ContextSize: 7}, config.Config{})
	testutil.Equal(t, noBudget.Tokens, 7)
	testutil.Nil(t, noBudget.DenominatorTokens)
	testutil.Nil(t, noBudget.Percent)
}
