package agent

import (
	"errors"
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestModelPolicy(t *testing.T) {
	cases := []struct {
		name    string
		backend config.Backend
		want    string
	}{
		{"configured wins over curated", config.Backend{Command: "claude", Models: []string{"x"}}, ModelPolicyConfigured},
		{"curated family", config.Backend{Command: "claude"}, ModelPolicyCurated},
		{"freeform custom", config.Backend{Command: "my-wrapper"}, ModelPolicyFreeform},
		{"freeform pi", config.Backend{Command: "pi"}, ModelPolicyFreeform},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, ModelPolicy(tc.backend), tc.want)
		})
	}
}

func TestValidateExplicitModel(t *testing.T) {
	cases := []struct {
		name    string
		backend config.Backend
		model   string
		allowed []string
		reject  bool
	}{
		{"empty always passes", config.Backend{Command: "claude"}, "", nil, false},
		{"whitespace passes", config.Backend{Command: "claude"}, "  ", nil, false},
		{"curated allowed", config.Backend{Command: "claude"}, "opus", nil, false},
		{"curated rejected", config.Backend{Command: "claude"}, "gpt-9", KnownModels("claude"), true},
		{"configured allowed", config.Backend{Command: "claude", Models: []string{"a", "b"}}, "b", nil, false},
		{"configured rejects curated id", config.Backend{Command: "claude", Models: []string{"a", "b"}}, "opus", []string{"a", "b"}, true},
		{"freeform accepts anything", config.Backend{Command: "my-wrapper"}, "whatever", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExplicitModel("be", tc.backend, tc.model)
			if !tc.reject {
				testutil.NoError(t, err)
				return
			}
			testutil.ErrorIs(t, err, ErrModelNotAllowed)
			var me *ModelNotAllowedError
			testutil.True(t, errors.As(err, &me))
			testutil.Equal(t, me.Backend, "be")
			testutil.Equal(t, me.RequestedModel, tc.model)
			testutil.DeepEqual(t, me.AllowedModels, tc.allowed)
		})
	}
}

func TestCreateAndStart_ExplicitModel(t *testing.T) {
	t.Run("rejected before worktree, DB row or session", func(t *testing.T) {
		repo := initGitRepo(t)
		d := createTestDB(t, repo)
		testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
		fr := &modelSnapshotRunner{}

		_, _, err := CreateAndStart(d, fr, CreateInput{Name: "rej", Project: "proj", Backend: "cl", Model: "gpt-9"})
		testutil.ErrorIs(t, err, ErrModelNotAllowed)
		testutil.Equal(t, fr.startCalls, 0)
		tasks, terr := d.Tasks()
		testutil.NoError(t, terr)
		testutil.Equal(t, len(tasks), 0)
		_, statErr := os.Stat(WorktreeDir("proj", "rej"))
		testutil.True(t, os.IsNotExist(statErr))
	})

	t.Run("allowed explicit model persists", func(t *testing.T) {
		repo := initGitRepo(t)
		d := createTestDB(t, repo)
		testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
		fr := &modelSnapshotRunner{}

		task, _, err := CreateAndStart(d, fr, CreateInput{Name: "ok", Project: "proj", Backend: "cl", Model: "opus"})
		testutil.NoError(t, err)
		testutil.Equal(t, task.Model, "opus")
		stored, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, stored.Model, "opus")
		testutil.Equal(t, fr.modelAtStart, "opus")
		testutil.Equal(t, fr.resolvedAtStart, "opus")
		RemoveWorktreeAndBranch(task.Worktree, task.Branch, repo)
	})

	t.Run("freeform backend accepts custom id", func(t *testing.T) {
		repo := initGitRepo(t)
		d := createTestDB(t, repo)
		fr := &modelSnapshotRunner{}

		task, _, err := CreateAndStart(d, fr, CreateInput{Name: "free", Project: "proj", Model: "my/custom"})
		testutil.NoError(t, err)
		testutil.Equal(t, task.Model, "my/custom")
		stored, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, stored.Model, "my/custom")
		testutil.Equal(t, fr.modelAtStart, "my/custom")
		testutil.Equal(t, fr.resolvedAtStart, "my/custom")
		RemoveWorktreeAndBranch(task.Worktree, task.Branch, repo)
	})
}

func TestResolveModel_ExplicitInvalidStillFallsOpenForPersistedTasks(t *testing.T) {
	task := &model.Task{ID: "t", Model: "gpt-9"}
	got, rp := ResolveModel(task, config.Backend{Command: "claude", Model: "sonnet"}, config.Config{})
	testutil.Equal(t, got, "sonnet")
	testutil.Nil(t, rp)
}

func TestValidateExplicitModel_FamilyMatrix(t *testing.T) {
	for _, c := range []struct{ command, valid, invalid string }{
		{"claude", "opus", "provider/arbitrary"},
		{"codex", "gpt-6.1-sol", "opus"},
		{"pi", "ollama/qwen3:8b", ""},
		{"opencode", "anthropic/claude-future", ""},
		{"agy", "provider/model", ""},
		{"cursor-agent", "provider/model", ""},
		{"omp", "provider/model", ""},
		{"my-wrapper", "provider/model", ""},
	} {
		t.Run(c.command, func(t *testing.T) {
			b := config.Backend{Command: c.command}
			testutil.NoError(t, ValidateExplicitModel("be", b, c.valid))
			if c.invalid != "" {
				testutil.ErrorIs(t, ValidateExplicitModel("be", b, c.invalid), ErrModelNotAllowed)
			}
			b.Models = []string{"configured-only"}
			testutil.NoError(t, ValidateExplicitModel("be", b, "configured-only"))
			testutil.ErrorIs(t, ValidateExplicitModel("be", b, c.valid), ErrModelNotAllowed)
		})
	}
}

type modelSnapshotRunner struct {
	fakeRunner
	modelAtStart    string
	resolvedAtStart string
}

func (r *modelSnapshotRunner) Start(task *model.Task, cfg config.Config, rows, cols uint16, resume bool) (SessionHandle, error) {
	r.modelAtStart = task.Model
	b, err := ResolveBackend(task, cfg)
	if err != nil {
		return nil, err
	}
	r.resolvedAtStart, _ = ResolveModel(task, b, cfg)
	return r.fakeRunner.Start(task, cfg, rows, cols, resume)
}

func TestCreateAndStart_ResolvedModelRejection(t *testing.T) {
	for _, source := range []string{"project", "default", "routing"} {
		t.Run(source, func(t *testing.T) {
			repo := initGitRepo(t)
			d := createTestDB(t, repo)
			testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
			switch source {
			case "project":
				testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD", Backend: "cl"}))
			case "default":
				testutil.NoError(t, d.SetConfigValue("defaults.backend", "cl"))
			case "routing":
				testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "cl", Probe: config.ProbeNone}}))
			}
			r := &fakeRunner{}
			task, _, err := CreateAndStart(d, r, CreateInput{Name: "reject", Project: "proj", Model: "gpt-9"})
			if task != nil {
				t.Cleanup(func() { RemoveWorktreeAndBranch(task.Worktree, task.Branch, repo) })
			}
			testutil.ErrorIs(t, err, ErrModelNotAllowed)
			testutil.Equal(t, r.startCalls, 0)
			tasks, err := d.Tasks()
			testutil.NoError(t, err)
			testutil.Equal(t, len(tasks), 0)
			_, err = os.Stat(WorktreeDir("proj", "reject"))
			testutil.True(t, os.IsNotExist(err))
		})
	}
}
