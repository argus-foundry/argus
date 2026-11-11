package agent

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func previewCfg() config.Config {
	cfg := config.DefaultConfig()
	cfg.Backends = map[string]config.Backend{
		"claude": {Command: "claude", Model: "sonnet"},
		"codex":  {Command: "codex"},
		"free":   {Command: "my-wrapper"},
	}
	cfg.Defaults.Backend = "claude"
	cfg.Projects = map[string]config.Project{
		"plain":  {Path: "/x"},
		"pinned": {Path: "/x", Backend: "codex"},
	}
	return cfg
}

func TestPreviewRouting_Cases(t *testing.T) {
	cases := []struct {
		name, project, backend string
		wantBackend, wantKind  string
		wantPolicy, wantDef    string
	}{
		{"inherited default", "plain", "", "claude", "claude", ModelPolicyCurated, "sonnet"},
		{"project override", "pinned", "", "codex", "codex", ModelPolicyCurated, ""},
		{"explicit backend beats project", "pinned", "claude", "claude", "claude", ModelPolicyCurated, "sonnet"},
		{"freeform backend", "plain", "free", "free", "", ModelPolicyFreeform, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := PreviewRouting(previewCfg(), tc.project, tc.backend, "", "")
			testutil.NoError(t, err)
			testutil.Equal(t, p.Backend, tc.wantBackend)
			testutil.Equal(t, p.Kind, tc.wantKind)
			testutil.Equal(t, p.ModelPolicy, tc.wantPolicy)
			testutil.Equal(t, p.DefaultModel, tc.wantDef)
			testutil.True(t, p.Models != nil)
		})
	}
}

func TestPreviewRouting_Errors(t *testing.T) {
	t.Run("unknown project", func(t *testing.T) {
		_, err := PreviewRouting(previewCfg(), "nope", "", "", "")
		testutil.ErrorIs(t, err, ErrPreviewProjectNotFound)
	})
	t.Run("unknown explicit backend", func(t *testing.T) {
		_, err := PreviewRouting(previewCfg(), "plain", "ghost", "", "")
		testutil.ErrorIs(t, err, ErrPreviewBackendUnavailable)
	})
	t.Run("no backend configured at all", func(t *testing.T) {
		cfg := previewCfg()
		cfg.Defaults.Backend = ""
		_, err := PreviewRouting(cfg, "plain", "", "", "")
		testutil.ErrorIs(t, err, ErrPreviewBackendUnavailable)
	})
}

func TestPreviewRouting_TierRoutingMatchesCreation(t *testing.T) {
	cfg := previewCfg()
	cfg.BackendRouting.Strategy = config.StrategyHeadroom
	cfg.BackendRouting.Tiers = []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
		{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 90},
	}
	for _, tc := range []struct {
		name          string
		claude, codex float64
		want          string
	}{
		{"claude has room", 10, 80, "claude"},
		{"claude exhausted", 99, 20, "codex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(backendtier.SetUsageSource(func() (backendtier.Usage, error) {
				return backendtier.Usage{
					Claude: map[string]usagebudget.Reading{"default": {Percentage: tc.claude}},
					Codex:  &usagebudget.Reading{Percentage: tc.codex},
				}, nil
			}))
			p, err := PreviewRouting(cfg, "plain", "", "", "")
			testutil.NoError(t, err)
			plan, err := ResolveTaskRouting(CreateInput{Project: "plain"}, cfg, false)
			testutil.NoError(t, err)
			testutil.Equal(t, p.Backend, tc.want)
			testutil.Equal(t, p.Backend, plan.Backend)
			testutil.Equal(t, p.Reason, plan.Reason)
		})
	}
}

func TestPreviewRouting_SameResultAsCreateAndStart(t *testing.T) {
	repo := initGitRepo(t)
	d := createTestDB(t, repo)
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude", Model: "opus"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD", Backend: "cl"}))

	p, err := PreviewRouting(d.Config(), "proj", "", "", "")
	testutil.NoError(t, err)
	task, _, err := CreateAndStart(d, &fakeRunner{}, CreateInput{Name: "same", Project: "proj"})
	testutil.NoError(t, err)
	t.Cleanup(func() { RemoveWorktreeAndBranch(task.Worktree, task.Branch, repo) })

	testutil.Equal(t, task.Backend, p.Backend)
	b, rerr := ResolveBackend(task, d.Config())
	testutil.NoError(t, rerr)
	m, _ := ResolveModel(task, b, d.Config())
	testutil.Equal(t, m, p.DefaultModel)
}

func TestPreviewRouting_NoSideEffects(t *testing.T) {
	repo := initGitRepo(t)
	d := createTestDB(t, repo)
	_, err := PreviewRouting(d.Config(), "proj", "", "code_slice", "")
	testutil.NoError(t, err)
	tasks, terr := d.Tasks()
	testutil.NoError(t, terr)
	testutil.Equal(t, len(tasks), 0)
	_, statErr := os.Stat(WorktreeDir("proj", "anything"))
	testutil.True(t, os.IsNotExist(statErr))
}

func TestCreateAndStart_RevalidatesModelAfterRoutingChange(t *testing.T) {
	repo := initGitRepo(t)
	d := createTestDB(t, repo)
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("cx", config.Backend{Command: "codex"}))
	testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD", Backend: "cl"}))

	p, err := PreviewRouting(d.Config(), "proj", "", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, p.Backend, "cl")
	testutil.True(t, len(p.Models) > 0)
	chosen := p.Models[0]

	testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD", Backend: "cx"}))
	fr := &fakeRunner{}
	_, _, err = CreateAndStart(d, fr, CreateInput{Name: "late", Project: "proj", Model: chosen})
	testutil.ErrorIs(t, err, ErrModelNotAllowed)
	testutil.Equal(t, fr.startCalls, 0)
}
