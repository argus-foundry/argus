package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResolveModelFromProfile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Defaults.Backend = "claude"
	backend := config.Backend{Command: "claude", Model: "haiku"}
	profile := &profiles.Profile{Name: "lean", Archetype: map[string]profiles.Archetype{"code_slice": {Models: map[string]string{"claude": "sonnet"}}}}
	tests := []struct {
		name, model, archetype string
		profile                *profiles.Profile
		backend                config.Backend
		want                   string
		overridden, profiled   bool
	}{
		{"override", "opus", "code_slice", profile, backend, "opus", true, false},
		{"invalid override", "gpt-nope", "code_slice", profile, backend, "sonnet", false, true},
		{"profile", "", "code_slice", profile, backend, "sonnet", false, true},
		{"no profile", "", "code_slice", nil, backend, "haiku", false, false},
		{"no archetype", "", "", profile, backend, "haiku", false, false},
		{"unknown archetype", "", "review", profile, backend, "haiku", false, false},
		{"cli default", "", "code_slice", nil, config.Backend{Command: "claude"}, "", false, false},
		{"invalid profile model", "", "code_slice", &profiles.Profile{Name: "bad", Archetype: map[string]profiles.Archetype{"code_slice": {Models: map[string]string{"claude": "gpt-nope"}}}}, backend, "haiku", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{Model: tc.model, Archetype: tc.archetype}
			got, rp, overridden := ResolveModelFromProfile(task, tc.backend, cfg, tc.profile)
			testutil.Equal(t, got, tc.want)
			testutil.Equal(t, overridden, tc.overridden)
			if tc.profiled {
				testutil.DeepEqual(t, rp, &ResolvedProfile{Name: "lean", Archetype: "code_slice", Model: "sonnet"})
			} else {
				testutil.Nil(t, rp)
			}
		})
	}
}
