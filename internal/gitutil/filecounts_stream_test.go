package gitutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestFileCountsBoundedAllocation(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "binary-prefix"}[binary], func(t *testing.T) {
			dir := t.TempDir()
			data := []byte(strings.Repeat("x", maxUntrackedCountBytes))
			if binary {
				data[0] = 0
			}
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "large"), data, 0o600))
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			got := countUntracked(dir, FileChange{Path: "large", Status: "??"})
			runtime.ReadMemStats(&after)
			if binary {
				testutil.Equal(t, got.Kind, FileKindBinary)
			} else {
				wantCounts(t, got, 1, 0)
			}
			testutil.Equal(t, after.TotalAlloc-before.TotalAlloc < 1<<20, true)
		})
	}
}
