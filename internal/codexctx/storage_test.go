package codexctx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestRolloutStoragePaths(t *testing.T) {
	for _, kind := range []string{"nested", "own directory alias", "foreign directory alias", "sibling prefix", "traversal"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			storage := filepath.Join(home, "sessions")
			testutil.NoError(t, os.Mkdir(storage, 0o700))
			dir := filepath.Join(storage, "2026", "11", "08")
			allowed := kind == "nested" || kind == "own directory alias"
			switch kind {
			case "foreign directory alias":
				dir = t.TempDir()
			case "sibling prefix":
				dir = filepath.Join(home, "sessions-foreign")
			case "traversal":
				dir = home
			}
			testutil.NoError(t, os.MkdirAll(dir, 0o700))
			path := filepath.Join(dir, "rollout-main.jsonl")
			raw := `{"type":"session_meta","payload":{"id":"main","cwd":"/w","timestamp":"2026-11-07T10:00:00Z"}}` + "\n" + tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 100)
			testutil.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
			if kind == "own directory alias" || kind == "foreign directory alias" {
				alias := filepath.Join(storage, "alias")
				testutil.NoError(t, os.Symlink(dir, alias))
				path = filepath.Join(alias, filepath.Base(path))
			}
			if kind == "traversal" {
				path = storage + "/../rollout-main.jsonl"
			}
			reading, _, ok, err := LatestForThread(home, "/w", Thread{ID: "main", RolloutPath: path})
			testutil.Equal(t, ok, allowed)
			if allowed {
				testutil.NoError(t, err)
				testutil.Equal(t, reading.UsedTokens, int64(15))
			} else {
				testutil.True(t, err != nil)
			}
		})
	}
}
