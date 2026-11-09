package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCaptureCodexSessionIDIn_SafeIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Run("missing index is not created", func(t *testing.T) {
		home := t.TempDir()
		id, err := CaptureCodexSessionIDIn(home, "/w")
		testutil.Error(t, err)
		testutil.Equal(t, id, "")
		_, err = os.Lstat(filepath.Join(home, codexStateDB))
		testutil.True(t, os.IsNotExist(err))
	})
	t.Run("index symlink is rejected", func(t *testing.T) {
		target := t.TempDir()
		seedCodexStateDB(t, target, "/w", "11111111-1111-4111-8111-111111111111")
		home := t.TempDir()
		testutil.NoError(t, os.Symlink(filepath.Join(target, codexStateDB), filepath.Join(home, codexStateDB)))
		id, err := CaptureCodexSessionIDIn(home, "/w")
		testutil.Error(t, err)
		testutil.Equal(t, id, "")
	})
}
