package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// limitCodexMoveMetadata shrinks the move metadata limit for one test. Callers
// must not be t.Parallel (the limit is a package var).
func limitCodexMoveMetadata(t *testing.T, n int64) {
	t.Helper()
	old := maxCodexMoveMetadataBytes
	maxCodexMoveMetadataBytes = n
	t.Cleanup(func() { maxCodexMoveMetadataBytes = old })
}

func TestCodexMoveLargeNativeMetadata(t *testing.T) {
	// Same boundary as production (default scanner buffer < header < limit <
	// header), just scaled down: 64 KiB bufio default, 256 KiB limit.
	const limit int64 = 256 << 10
	limitCodexMoveMetadata(t, limit)
	for _, tc := range []struct {
		name   string
		size   int
		reject bool
	}{
		{"70KiB", 70 << 10, false}, {"underLimit", int(limit) - 4<<10, false}, {"overLimit", int(limit) + 4<<10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			header, err := json.Marshal(map[string]any{"timestamp": "2026-11-07T10:00:00Z", "type": "session_meta", "payload": map[string]any{
				"id": codexMoveSID, "cwd": f.worktree, "timestamp": "2026-11-07T10:00:00Z", "source": "cli",
				"base_instructions": map[string]string{"text": strings.Repeat("instruction ", tc.size/12+1)},
			}})
			testutil.NoError(t, err)
			body := append(append(header, '\n'), []byte("{\"type\":\"event_msg\",\"payload\":{}}\n")...)
			testutil.True(t, len(header) > 64<<10)
			testutil.NoError(t, os.WriteFile(f.rollout, body, 0600))
			before := fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB))
			_, res, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			if tc.reject {
				testutil.True(t, int64(len(header)) > limit)
				testutil.ErrorIs(t, err, ErrAccount)
				got, getErr := f.d.Get(f.task.ID)
				testutil.NoError(t, getErr)
				testutil.Equal(t, got.Account, "")
				testutil.Equal(t, got.SessionID, codexMoveSID)
				_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
				testutil.False(t, found)
				_, statErr := os.Stat(filepath.Join(f.dstHome, "sessions"))
				testutil.True(t, os.IsNotExist(statErr))
			} else {
				testutil.NoError(t, err)
				testutil.True(t, res.Resumed)
				dst, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
				testutil.True(t, found)
				copied, readErr := os.ReadFile(dst)
				testutil.NoError(t, readErr)
				testutil.DeepEqual(t, copied, body)
			}
			testutil.DeepEqual(t, fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB)), before)
			after, readErr := os.ReadFile(f.rollout)
			testutil.NoError(t, readErr)
			testutil.DeepEqual(t, after, body)
		})
	}
}
