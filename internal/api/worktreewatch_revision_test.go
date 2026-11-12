package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWtRevisionFingerprint(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a b.txt")
	testutil.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	msg := gitutil.GitStatusRefreshMsg{Status: ` M "a b.txt"`}
	first := worktreeRevision(root, msg)
	later := time.Now().Add(time.Second)
	testutil.NoError(t, os.Chtimes(path, later, later))
	testutil.Equal(t, worktreeRevision(root, msg) != first, true)
	var lines []string
	for i := range wtFingerprintLimit + 1 {
		lines = append(lines, fmt.Sprint("?? f", i))
	}
	large := gitutil.GitStatusRefreshMsg{Status: strings.Join(lines, "\n")}
	testutil.Equal(t, worktreeRevision(root, large), statusRevision(large))
	testutil.Equal(t, worktreeRevision(filepath.Join(root, "absent"), msg), statusRevision(msg))
}

func TestStatusRevisionCountsOnly(t *testing.T) {
	one, two, zero := 1, 2, 0
	msg := gitutil.GitStatusRefreshMsg{Status: "?? f", Files: []gitutil.FileChange{{Path: "f", Status: "??", Kind: gitutil.FileKindText, Additions: &one, Deletions: &zero}}}
	first := statusRevision(msg)
	msg.Files[0].Additions = &two
	testutil.Equal(t, statusRevision(msg) != first, true)
}
