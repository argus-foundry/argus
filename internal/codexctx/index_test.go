package codexctx

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestOpenThreadIndexRejectsSpecialFiles(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			for _, kind := range []string{"fifo", "directory", "symlink"} {
				t.Run(kind, func(t *testing.T) {
					path := makeIndex(t)
					target := path + suffix
					if suffix == "" {
						testutil.NoError(t, os.Remove(path))
					}
					switch kind {
					case "fifo":
						testutil.NoError(t, syscall.Mkfifo(target, 0o600))
					case "directory":
						testutil.NoError(t, os.Mkdir(target, 0o700))
					case "symlink":
						dest := filepath.Join(t.TempDir(), "regular")
						testutil.NoError(t, os.WriteFile(dest, nil, 0o600))
						testutil.NoError(t, os.Symlink(dest, target))
					}
					conn, err := OpenThreadIndex(path)
					if conn != nil {
						testutil.NoError(t, conn.Close())
					}
					testutil.True(t, err != nil)
				})
			}
		})
	}
}

func TestFindThreadReadsUncheckpointedWAL(t *testing.T) {
	path := makeIndex(t)
	writer, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	defer writer.Close() //nolint:errcheck // fixture writer
	_, err = writer.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; INSERT INTO threads VALUES ('main','/w','/r/rollout-main.jsonl',1,1,'cli')`)
	testutil.NoError(t, err)
	wal, err := os.Stat(path + "-wal")
	testutil.NoError(t, err)
	testutil.True(t, wal.Size() > 0)
	thread, ok, err := FindThread(path, "/w", "")
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, thread.ID, "main")
	reader, err := OpenThreadIndex(path)
	testutil.NoError(t, err)
	defer reader.Close() //nolint:errcheck // read-only fixture
	_, err = reader.Exec(`DELETE FROM threads`)
	testutil.True(t, err != nil)
}
