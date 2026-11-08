package codexctx

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LatestInFile reads a regular rollout without following a file symlink or
// waiting for a FIFO writer. The descriptor check also covers replacement races.
func LatestInFile(path string) (Reading, bool, error) {
	if !rolloutName(path) {
		return Reading{}, false, fmt.Errorf("codexctx: invalid rollout filename")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return Reading{}, false, nil
		}
		return Reading{}, false, err
	}
	defer f.Close() //nolint:errcheck // read-only handle
	return latestRegular(f)
}

func rolloutName(path string) bool {
	return strings.HasPrefix(filepath.Base(path), "rollout-") && filepath.Ext(path) == ".jsonl"
}

func latestRegular(f *os.File) (Reading, bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return Reading{}, false, err
	}
	if !fi.Mode().IsRegular() {
		return Reading{}, false, fmt.Errorf("codexctx: rollout is not a regular file")
	}
	return Latest(f, fi.Size())
}

// LatestForThread verifies account storage and the file's session identity
// before reading counts. Session-directory symlinks used by Argus overlays are allowed.
func LatestForThread(home, cwd string, thread Thread, additionalHomes ...string) (Reading, time.Time, bool, error) {
	f, err := openAccountRollout(append([]string{home}, additionalHomes...), thread.RolloutPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Reading{}, time.Time{}, false, nil
		}
		return Reading{}, time.Time{}, false, err
	}
	defer f.Close() //nolint:errcheck // read-only handle
	fi, err := f.Stat()
	if err != nil {
		return Reading{}, time.Time{}, false, err
	}
	if !fi.Mode().IsRegular() {
		return Reading{}, time.Time{}, false, fmt.Errorf("codexctx: rollout is not a regular file")
	}
	// Session metadata is the first record. Bound it independently of the tail.
	raw, err := bufio.NewReader(io.LimitReader(f, 64<<10)).ReadBytes('\n')
	if err != nil {
		return Reading{}, time.Time{}, false, fmt.Errorf("codexctx: missing session metadata")
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ID        string    `json:"id"`
			Cwd       string    `json:"cwd"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &meta) != nil || meta.Type != "session_meta" || meta.Payload.ID != thread.ID || meta.Payload.Cwd != cwd || meta.Payload.Timestamp.IsZero() {
		return Reading{}, time.Time{}, false, fmt.Errorf("codexctx: rollout session identity mismatch")
	}
	r, ok, err := Latest(f, fi.Size())
	return r, meta.Payload.Timestamp, ok, err
}

func openAccountRollout(homes []string, path string) (*os.File, error) {
	if !filepath.IsAbs(path) || !rolloutName(path) {
		return nil, fmt.Errorf("codexctx: invalid rollout path")
	}
	// Resolve directory aliases, then traverse from an open storage descriptor.
	// Every subsequent component uses the kernel no-follow flag.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	for _, home := range homes {
		for _, dir := range []string{"sessions", "archived_sessions"} {
			storage, err := filepath.EvalSymlinks(filepath.Join(home, dir))
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(storage, path)
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
			return openStorageRollout(storage, rel)
		}
	}
	return nil, fmt.Errorf("codexctx: rollout outside account session storage")
}
