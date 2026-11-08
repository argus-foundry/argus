package codexctx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func TestLatestForThread(t *testing.T) {
	for _, kind := range []string{"valid", "missing", "invalid filename", "relative", "outside account", "file symlink", "directory", "fifo", "no metadata", "mismatched id", "mismatched cwd", "missing creation", "oversize metadata", "archived", "directory alias"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			storage := filepath.Join(home, "sessions")
			testutil.NoError(t, os.Mkdir(storage, 0o700))
			if kind == "archived" {
				storage = filepath.Join(home, "archived_sessions")
				testutil.NoError(t, os.Mkdir(storage, 0o700))
			}
			if kind == "directory alias" {
				testutil.NoError(t, os.Remove(storage))
				testutil.NoError(t, os.Symlink(t.TempDir(), storage))
			}
			path := filepath.Join(storage, "rollout-main.jsonl")
			id, cwd, created := "main", "/w", "2026-11-07T10:00:00Z"
			switch kind {
			case "mismatched id":
				id = "foreign"
			case "mismatched cwd":
				cwd = "/other"
			case "missing creation":
				created = "0001-01-01T00:00:00Z"
			}
			raw := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":%q}}`+"\n", id, cwd, created) + tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 100)
			if kind == "no metadata" {
				raw = tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 100)
			}
			if kind == "oversize metadata" {
				raw = strings.Repeat("x", 64<<10) + "\n" + raw
			}
			switch kind {
			case "directory":
				testutil.NoError(t, os.Mkdir(path, 0o700))
			case "fifo":
				testutil.NoError(t, syscall.Mkfifo(path, 0o600))
			case "file symlink":
				target := filepath.Join(t.TempDir(), "rollout-target.jsonl")
				testutil.NoError(t, os.WriteFile(target, []byte(raw), 0o600))
				testutil.NoError(t, os.Symlink(target, path))
			case "missing":
			default:
				testutil.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
			}
			switch kind {
			case "relative":
				path = "rollout-main.jsonl"
			case "invalid filename":
				path = filepath.Join(storage, "auth.json")
			case "outside account":
				path = filepath.Join(t.TempDir(), "rollout-foreign.jsonl")
				testutil.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
			}
			reading, at, ok, err := LatestForThread(home, "/w", Thread{ID: "main", RolloutPath: path})
			switch kind {
			case "valid", "archived", "directory alias":
				testutil.NoError(t, err)
				testutil.True(t, ok)
				testutil.Equal(t, reading.UsedTokens, int64(15))
				testutil.True(t, at.Equal(time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)))
			case "missing":
				testutil.NoError(t, err)
				testutil.False(t, ok)
			default:
				testutil.True(t, err != nil)
				testutil.False(t, ok)
			}
		})
	}
}

func TestRolloutReadersRejectRelativeFileSymlink(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	testutil.NoError(t, os.Mkdir(dir, 0o700))
	target := filepath.Join(dir, "rollout-real.jsonl")
	alias := filepath.Join(dir, "rollout-alias.jsonl")
	raw := `{"type":"session_meta","payload":{"id":"main","cwd":"/w","timestamp":"2026-11-07T10:00:00Z"}}` + "\n" + tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 100)
	testutil.NoError(t, os.WriteFile(target, []byte(raw), 0o600))
	testutil.NoError(t, os.Symlink(filepath.Base(target), alias))
	t.Run("standalone", func(t *testing.T) {
		_, ok, err := LatestInFile(alias)
		testutil.True(t, err != nil)
		testutil.False(t, ok)
	})
	t.Run("scoped", func(t *testing.T) {
		_, _, ok, err := LatestForThread(home, "/w", Thread{ID: "main", RolloutPath: alias})
		testutil.True(t, err != nil)
		testutil.False(t, ok)
	})
}
