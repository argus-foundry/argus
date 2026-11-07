package gitutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedSnapshotSwapBeforeOpen(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "inside")
	testutil.NoError(t, os.Mkdir(inside, 0o700))
	testutil.NoError(t, os.WriteFile(filepath.Join(inside, "secret"), []byte("INSIDE\n"), 0o600))
	outside := t.TempDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("OUTSIDE\n"), 0o600))
	root, err := os.OpenRoot(dir)
	testutil.NoError(t, err)
	defer func() { testutil.NoError(t, root.Close()) }()
	relative, err := untrackedRelativePath(dir, filepath.Join(inside, "secret"))
	testutil.NoError(t, err)
	testutil.NoError(t, os.Rename(inside, filepath.Join(dir, "saved")))
	testutil.NoError(t, os.Symlink(outside, inside))
	destination := filepath.Join(t.TempDir(), "snapshot")
	remaining := int64(maxUntrackedDiffBytes)
	err = copyUntrackedFile(root, relative, destination, &remaining)
	testutil.Error(t, err)
	_, err = os.Stat(destination)
	testutil.ErrorIs(t, err, os.ErrNotExist)
}

func TestUntrackedSnapshotRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	testutil.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600))
	root, err := os.OpenRoot(dir)
	testutil.NoError(t, err)
	defer func() { testutil.NoError(t, root.Close()) }()
	destination := filepath.Join(t.TempDir(), "snapshot")
	done := make(chan error, 1)
	go func() {
		remaining := int64(maxUntrackedDiffBytes)
		done <- copyUntrackedFile(root, "pipe", destination, &remaining)
	}()
	select {
	case err := <-done:
		testutil.ErrorIs(t, err, errUntrackedUnavailable)
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot blocked opening a non-regular file")
	}
}

func TestUntrackedFinalEntryTransitions(t *testing.T) {
	for _, before := range []string{"file", "directory", "link"} {
		for _, after := range []string{"file", "directory", "link", "outside-link"} {
			t.Run(before+"/"+after, func(t *testing.T) {
				dir := t.TempDir()
				outside := filepath.Join(t.TempDir(), "secret")
				testutil.NoError(t, os.WriteFile(outside, []byte("OUTSIDE\n"), 0o600))
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, "inside"), []byte("INSIDE\n"), 0o600))
				entry := filepath.Join(dir, "entry")
				create := func(kind string) {
					switch kind {
					case "file":
						testutil.NoError(t, os.WriteFile(entry, []byte("REPLACEMENT\n"), 0o600))
					case "directory":
						testutil.NoError(t, os.Mkdir(entry, 0o700))
					case "link":
						testutil.NoError(t, os.Symlink("inside", entry))
					case "outside-link":
						testutil.NoError(t, os.Symlink(outside, entry))
					}
				}
				create(before)
				root, err := os.OpenRoot(dir)
				testutil.NoError(t, err)
				defer func() { testutil.NoError(t, root.Close()) }()
				info, err := root.Lstat("entry")
				testutil.NoError(t, err)
				testutil.NoError(t, os.Remove(entry))
				create(after)
				destination := filepath.Join(t.TempDir(), "snapshot")
				remaining := int64(maxUntrackedDiffBytes)
				link, err := captureUntrackedEntry(root, dir, "entry", "entry", destination, info.Mode(), &remaining)
				if after == "directory" || after == "outside-link" || before == "link" && after != "link" {
					testutil.ErrorIs(t, err, errUntrackedUnavailable)
					_, err = os.Stat(destination)
					testutil.ErrorIs(t, err, os.ErrNotExist)
					return
				}
				testutil.NoError(t, err)
				data, err := os.ReadFile(destination)
				testutil.NoError(t, err)
				expected := "REPLACEMENT\n"
				if after == "link" {
					expected = "INSIDE\n"
					if link {
						expected = "inside"
					}
				}
				testutil.Equal(t, string(data), expected)
			})
		}
	}
}
