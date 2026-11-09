package agent

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestLoadSessionSizes_ParsesAndIgnoresTornOrBackwardLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(SessionsDir(), 0o700))
	body := "0 282 69\n100 142 38\nnot a line\n90 99 99\n250 200 50\n300 14" // last line torn
	testutil.NoError(t, os.WriteFile(SessionSizesPath("t"), []byte(body), 0o600))

	got := LoadSessionSizes("t")
	testutil.DeepEqual(t, got, []SizeMark{{0, 282, 69}, {100, 142, 38}, {250, 200, 50}})

	if LoadSessionSizes("missing") != nil {
		t.Fatal("missing index must load as nil")
	}
}

func TestSizeMarkAt(t *testing.T) {
	marks := []SizeMark{{0, 282, 69}, {100, 142, 38}, {250, 200, 50}}
	for _, tc := range []struct {
		off  int64
		want int
	}{{0, 282}, {99, 282}, {100, 142}, {249, 142}, {250, 200}, {9999, 200}} {
		m, ok := SizeMarkAt(marks, tc.off)
		testutil.Equal(t, ok, true)
		testutil.Equal(t, m.Cols, tc.want)
	}
	_, ok := SizeMarkAt(nil, 5)
	testutil.Equal(t, ok, false)
	_, ok = SizeMarkAt([]SizeMark{{10, 80, 24}}, 5) // before the first mark
	testutil.Equal(t, ok, false)
}

// A session records its initial size at offset 0 and a mark for each resize at
// the log byte count so far; repeating a size adds nothing (a drag that
// reports the same size every frame must not grow the index).
func TestSession_Resize_AppendsIndexAtLogOffset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sess, err := StartSession("idx", exec.Command("sh", "-c", "printf hello; sleep 10"), 24, 80)
	testutil.NoError(t, err)
	defer sess.Stop() //nolint:errcheck

	deadline := time.Now().Add(5 * time.Second)
	for sess.logOff.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	testutil.Equal(t, sess.logOff.Load() >= 5, true)
	off := sess.logOff.Load()

	testutil.NoError(t, sess.Resize(30, 100))
	testutil.NoError(t, sess.Resize(30, 100)) // same size: no new line
	testutil.NoError(t, sess.Resize(24, 80))

	got := LoadSessionSizes("idx")
	testutil.DeepEqual(t, got, []SizeMark{{0, 80, 24}, {off, 100, 30}, {off, 80, 24}})
}

// Starting a session truncates the previous process's index along with its log.
func TestStartSession_ResetsIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(SessionsDir(), 0o700))
	testutil.NoError(t, os.WriteFile(SessionSizesPath("reset"), []byte("0 500 90\n10 400 80\n"), 0o600))
	sess, err := StartSession("reset", exec.Command("sleep", "10"), 24, 80)
	testutil.NoError(t, err)
	defer sess.Stop() //nolint:errcheck
	testutil.DeepEqual(t, LoadSessionSizes("reset"), []SizeMark{{0, 80, 24}})
}

func TestPruneCompleted_RemovesSessionSizesIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.Add(&model.Task{ID: "prune-idx", Name: "done", Status: model.StatusComplete, Project: "proj"}))
	ResetSessionSizes("prune-idx", 80, 24)
	_, err = PruneCompleted(d, PruneOptions{})
	testutil.NoError(t, err)
	if _, statErr := os.Stat(SessionSizesPath("prune-idx")); statErr == nil {
		t.Error("prune should remove the sizes index")
	}
}
