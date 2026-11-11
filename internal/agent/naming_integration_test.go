package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/llm"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRunAutoRenameForTask(t *testing.T) {
	for _, outcome := range []string{"success", "error", "unsupported", "manual rename"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			id := addTask(t, d, "original-slug")
			task := model.Task{ID: id, Name: "original-slug", Backend: "selected"}
			cfg := config.Config{Backends: map[string]config.Backend{"selected": {Command: "codex --model gpt-6-astra"}}}
			if outcome == "unsupported" {
				cfg.Backends["selected"] = config.Backend{Command: "pi"}
			}
			previous := namingGenerateFn
			t.Cleanup(func() { namingGenerateFn = previous })
			calls := 0
			namingGenerateFn = func(ctx context.Context, prompt string, backend llm.NamingBackend) (string, error) {
				calls++
				_, deadline := ctx.Deadline()
				testutil.Equal(t, deadline, true)
				testutil.Equal(t, prompt, "actual task prompt")
				testutil.Equal(t, backend.Binary, "codex")
				testutil.Contains(t, backend.Args(prompt, "instructions")[2], "gpt-6-luna")
				if outcome == "error" {
					return "", errors.New("model unavailable")
				}
				if outcome == "manual rename" {
					ok, err := d.RenameIfName(id, "original-slug", "user-name")
					testutil.NoError(t, err)
					testutil.Equal(t, ok, true)
				}
				return "backend-aware-naming", nil
			}
			runAutoRenameForTask(d, task, "actual task prompt", cfg)
			got, err := d.Get(id)
			testutil.NoError(t, err)
			want := "original-slug"
			if outcome == "success" {
				want = "backend-aware-naming"
			}
			if outcome == "manual rename" {
				want = "user-name"
			}
			testutil.Equal(t, got.Name, want)
			expectedCalls := 1
			if outcome == "unsupported" {
				expectedCalls = 0
			}
			testutil.Equal(t, calls, expectedCalls)
		})
	}
}
