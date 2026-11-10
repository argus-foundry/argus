package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestValidateScheduleModel(t *testing.T) {
	for _, tc := range []struct {
		name, command, model string
		models               []string
		reject               bool
	}{
		{"empty", "claude", "", nil, false},
		{"whitespace", "claude", "  ", nil, false},
		{"curated allowed", "codex", "gpt-6.1-sol", nil, false},
		{"curated rejected", "codex", "opus", nil, true},
		{"freeform allowed", "pi", "provider/model", nil, false},
		{"configured allowed", "pi", "only", []string{"only"}, false},
		{"configured rejected", "pi", "provider/model", []string{"only"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Backends["be"] = config.Backend{Command: tc.command, Models: tc.models}
			err := ValidateScheduleModel(&model.ScheduledTask{Backend: "be", Model: tc.model}, cfg)
			if tc.reject {
				testutil.ErrorIs(t, err, ErrModelNotAllowed)
			} else {
				testutil.NoError(t, err)
			}
		})
	}
	t.Run("missing backend", func(t *testing.T) {
		err := ValidateScheduleModel(&model.ScheduledTask{Backend: "missing", Model: "opus"}, config.DefaultConfig())
		testutil.NotNil(t, err)
		testutil.Contains(t, err.Error(), `backend "missing" not found`)
	})
}
