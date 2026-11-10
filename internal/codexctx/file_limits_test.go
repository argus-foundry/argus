package codexctx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestValidateThreadRolloutBudgets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int64
		useDefault bool
		wantError  bool
	}{
		{"default64KiB", 0, true, true},
		{"explicit64KiB", 64 << 10, false, true},
		{"explicit4MiB", 4 << 20, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout-thread.jsonl")
			header, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread", "cwd": "/workspace", "timestamp": "2026-11-07T10:00:00Z", "base_instructions": map[string]string{"text": strings.Repeat("instruction ", 6000)}}})
			testutil.NoError(t, err)
			testutil.True(t, len(header) > 64<<10)
			testutil.NoError(t, os.WriteFile(path, append(header, '\n'), 0600))
			input, err := os.Open(path)
			testutil.NoError(t, err)
			defer func() { _ = input.Close() }()
			thread := Thread{ID: "thread", RolloutPath: path}
			if tc.useDefault {
				_, err = ValidateThreadRollout(input, "/workspace", thread)
			} else {
				_, err = ValidateThreadRolloutWithLimit(input, "/workspace", thread, tc.limit)
			}
			if tc.wantError {
				testutil.Error(t, err)
			} else {
				testutil.NoError(t, err)
			}
		})
	}
}
