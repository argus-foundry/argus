package inject

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTrustClaudeWorktree(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		bad         bool
	}{
		{"missing", "", false},
		{"existing", `{"token":"keep","large":9007199254740993,"projects":{"/other":{"x":[1,null]},"WORKTREE":{"allowedTools":["Read"],"hasTrustDialogAccepted":false}}}`, false},
		{"malformed", `{broken`, true},
		{"null config", `null`, true},
		{"null projects", `{"projects":null}`, true},
		{"bad projects", `{"projects":[]}`, true},
		{"bad project", `{"projects":{"WORKTREE":false}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			wt := t.TempDir()
			canonical, err := filepath.EvalSymlinks(wt)
			testutil.NoError(t, err)
			input := strings.ReplaceAll(tc.input, "WORKTREE", canonical)
			path := filepath.Join(os.Getenv("HOME"), ".claude.json")
			if input != "" {
				testutil.NoError(t, os.WriteFile(path, []byte(input), 0o640))
			}
			err = TrustClaudeWorktree(path, wt)
			raw, readErr := os.ReadFile(path)
			testutil.NoError(t, readErr)
			if tc.bad {
				testutil.NotNil(t, err)
				testutil.Equal(t, string(raw), input)
				return
			}
			testutil.NoError(t, err)
			var got map[string]json.RawMessage
			testutil.NoError(t, json.Unmarshal(raw, &got))
			var projects map[string]map[string]json.RawMessage
			testutil.NoError(t, json.Unmarshal(got["projects"], &projects))
			testutil.Equal(t, string(projects[canonical]["hasTrustDialogAccepted"]), "true")
			if input != "" {
				decode := func(raw []byte) map[string]any {
					var data map[string]any
					d := json.NewDecoder(bytes.NewReader(raw))
					d.UseNumber()
					testutil.NoError(t, d.Decode(&data))
					return data
				}
				want := decode([]byte(input))
				want["projects"].(map[string]any)[canonical].(map[string]any)["hasTrustDialogAccepted"] = true
				testutil.DeepEqual(t, decode(raw), want)
				testutil.Equal(t, string(got["large"]), "9007199254740993")
				testutil.Equal(t, string(got["token"]), `"keep"`)
				var tools []string
				testutil.NoError(t, json.Unmarshal(projects[canonical]["allowedTools"], &tools))
				testutil.DeepEqual(t, tools, []string{"Read"})
				testutil.Equal(t, len(projects), 2)
				info, err := os.Stat(path)
				testutil.NoError(t, err)
				testutil.Equal(t, info.Mode().Perm(), os.FileMode(0o640))
			}
			testutil.NoError(t, TrustClaudeWorktree(path, wt))
			again, err := os.ReadFile(path)
			testutil.NoError(t, err)
			testutil.Equal(t, string(again), string(raw))
		})
	}
}

func TestTrustClaudeWorktree_Paths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wt := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	testutil.NoError(t, os.Symlink(wt, link))
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	testutil.NoError(t, TrustClaudeWorktree(path, link))
	raw, err := os.ReadFile(path)
	testutil.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(wt)
	testutil.NoError(t, err)
	testutil.Contains(t, string(raw), canonical)
	testutil.Equal(t, strings.Contains(string(raw), link), false)
	testutil.NotNil(t, TrustClaudeWorktree(path, filepath.Join(wt, "missing")))
	// Reject config symlinks and keep their targets untouched.
	linkedConfig := filepath.Join(t.TempDir(), "config")
	testutil.NoError(t, os.Symlink(path, linkedConfig))
	testutil.NotNil(t, TrustClaudeWorktree(linkedConfig, wt))
	again, err := os.ReadFile(path)
	testutil.NoError(t, err)
	testutil.Equal(t, string(again), string(raw))
	// A regular file cannot be the config parent; no writes escape elsewhere.
	testutil.NotNil(t, TrustClaudeWorktree(filepath.Join(path, "child"), wt))
}
