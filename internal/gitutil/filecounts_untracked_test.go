package gitutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestFileCountsEntryTransitions(t *testing.T) {
	for _, after := range []string{"outside-link", "inside-link", "fifo", "directory", "replacement", "growth", "removed"} {
		t.Run(after, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "entry", "small\n")
			writeFile(t, dir, "inside", "inside\n")
			outside := filepath.Join(t.TempDir(), "secret")
			testutil.NoError(t, os.WriteFile(outside, []byte("outside\nsecret\n"), 0o600))
			root, err := os.OpenRoot(dir)
			testutil.NoError(t, err)
			defer func() { testutil.NoError(t, root.Close()) }()
			info, err := root.Lstat("entry")
			testutil.NoError(t, err)
			path := filepath.Join(dir, "entry")
			testutil.NoError(t, os.Rename(path, filepath.Join(dir, "saved")))
			switch after {
			case "outside-link":
				testutil.NoError(t, os.Symlink(outside, path))
			case "inside-link":
				testutil.NoError(t, os.Symlink("inside", path))
			case "fifo":
				testutil.NoError(t, syscall.Mkfifo(path, 0o600))
			case "directory":
				testutil.NoError(t, os.Mkdir(path, 0o700))
			case "replacement":
				writeFile(t, dir, "entry", "replacement\n")
			case "growth":
				testutil.NoError(t, os.Rename(filepath.Join(dir, "saved"), path))
				testutil.NoError(t, os.Truncate(path, maxUntrackedCountBytes+1))
			}
			done := make(chan FileChange, 1)
			go func() {
				done <- countUntrackedEntry(root, FileChange{Path: "entry", Kind: FileKindUnavailable}, info, false)
			}()
			select {
			case got := <-done:
				wantUnavailableCount(t, got)
			case <-time.After(2 * time.Second):
				t.Fatal("count blocked on replaced entry")
			}
		})
	}
}

func TestFileCountsGrowthAfterStat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "growing", "before\n")
	f, err := os.Open(filepath.Join(dir, "growing"))
	testutil.NoError(t, err)
	defer f.Close()
	info, err := f.Stat()
	testutil.NoError(t, err)
	testutil.Equal(t, info.Size() < maxUntrackedCountBytes, true)
	// Grow the same opened file after admission, before the counting read.
	writeFile(t, dir, "growing", strings.Repeat("x", maxUntrackedCountBytes+1))
	wantUnavailableCount(t, countUntrackedContent(f, FileChange{Path: "growing"}, false))
}

type prefixOnlyCountReader struct{ reads int }

func (r *prefixOnlyCountReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		return 0, errors.New("binary tail must not be read")
	}
	clear(p)
	return len(p), nil
}

func TestFileCountsReaderBoundaries(t *testing.T) {
	t.Run("binary prefix", func(t *testing.T) {
		r := &prefixOnlyCountReader{}
		got := countUntrackedContent(r, FileChange{}, false)
		testutil.Equal(t, got.Kind, FileKindBinary)
		testutil.Equal(t, r.reads, 1)
	})
	for _, size := range []int{0, binarySniffBytes - 1, binarySniffBytes, binarySniffBytes + 1, maxUntrackedCountBytes, maxUntrackedCountBytes + 1} {
		t.Run(strings.Repeat("x", size%10)+string(rune('a'+size%26)), func(t *testing.T) {
			data := strings.Repeat("a", size)
			got := countUntrackedContent(strings.NewReader(data), FileChange{}, false)
			if size > maxUntrackedCountBytes {
				wantUnavailableCount(t, got)
			} else {
				lines := 1
				if size == 0 {
					lines = 0
				}
				wantCounts(t, got, lines, 0)
			}
		})
	}
	t.Run("read error", func(t *testing.T) {
		wantUnavailableCount(t, countUntrackedContent(io.MultiReader(strings.NewReader(strings.Repeat("x", 8000)), errorCountReader{}), FileChange{}, false))
	})
}

type errorCountReader struct{}

func (errorCountReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
