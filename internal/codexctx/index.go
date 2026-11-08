package codexctx

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
)

// MainThreadSource identifies a top-level Codex CLI conversation.
const MainThreadSource = "cli"

// OpenThreadIndex preserves SQLite's normal WAL lookup and never creates an index.
func OpenThreadIndex(path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := checkIndexFile(path + suffix); err != nil {
			if suffix != "" && os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
	}
	// Accepted residual: an owner of this Codex home can replace files after preflight.
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	return sql.Open("sqlite", uri.String())
}

func checkIndexFile(path string) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("codexctx: index file is not regular")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only descriptor
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return fmt.Errorf("codexctx: index changed during preflight")
	}
	return nil
}
