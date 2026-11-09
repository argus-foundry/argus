package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraExplicitNullKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, d := testServer(t)
	task := &model.Task{Name: "worker"}
	testutil.NoError(t, d.Add(task))
	_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: mustOrch(t, d, "qa"), Name: "worker", Kind: db.HeraKindWorker}, task.ID, t.TempDir())
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyContextSize, "120000"))
	var raw struct {
		Orchestrators []struct {
			Roles []map[string]json.RawMessage `json:"roles"`
		} `json:"orchestrators"`
	}
	testutil.NoError(t, json.Unmarshal([]byte(getHeraRaw(t, srv)), &raw))
	role := raw.Orchestrators[0].Roles[0]
	var resolution, context map[string]json.RawMessage
	testutil.NoError(t, json.Unmarshal(role["model_resolution"], &resolution))
	testutil.NoError(t, json.Unmarshal(role["context"], &context))
	for _, key := range []string{"effective_model", "warning"} {
		t.Run(key, func(t *testing.T) {
			value, present := resolution[key]
			testutil.True(t, present)
			testutil.Equal(t, string(value), "null")
		})
	}
	value, present := context["observed_at"]
	testutil.True(t, present)
	testutil.Equal(t, string(value), "null")
}

func TestHeraFreelanceProjection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, d := testServer(t)
	testutil.NoError(t, d.SetConfigValue("defaults.backend", "claude"))
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude"}))
	dir := filepath.Join(db.DataDir(), "profiles")
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "default.toml"), []byte("[archetype.code_slice]\nmodels = { claude = \"sonnet\" }\neffort = \"high\"\n"), 0o644))
	task := &model.Task{Name: "free", Archetype: "code_slice", Model: "opus"}
	testutil.NoError(t, d.Add(task))
	_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: mustOrch(t, d, "qa"), Name: "free", Kind: db.HeraKindFreelance, Archetype: "code_slice"}, task.ID, t.TempDir())
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyContextSize, "120000"))
	resp := getHera(t, srv)
	testutil.Equal(t, len(resp.Freelance), 1)
	r := resp.Freelance[0]
	testutil.Equal(t, r.Archetype, "code_slice")
	testutil.Equal(t, r.ModelResolution.RequestedModel, "opus")
	testutil.Equal(t, r.ModelResolution.Effort, "high")
	testutil.Equal(t, r.ModelResolution.Profile, "default")
	testutil.Nil(t, r.ModelResolution.Warning)
	testutil.Equal(t, r.ModelResolution.Source, "task_override")
	testutil.Equal(t, r.ModelResolution.Basis, "current_config")
	testutil.NotNil(t, r.ModelResolution.EffectiveModel)
	testutil.Equal(t, *r.ModelResolution.EffectiveModel, "opus")
	testutil.NotNil(t, r.Context)
	testutil.Equal(t, r.Context.Tokens, 120000)
	testutil.Equal(t, r.Context.DenominatorKind, "worker_window")
	testutil.Equal(t, *r.Context.DenominatorTokens, 1000000)
	testutil.Equal(t, *r.Context.Percent, 12)
	testutil.Nil(t, r.Context.ObservedAt)
}
