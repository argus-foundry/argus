package codexctx

import (
	"io"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

type usageBoundReader struct{ bytes int }

func (r *usageBoundReader) ReadAt(p []byte, off int64) (int, error) {
	r.bytes = len(p)
	return 0, io.EOF
}
func TestUsage_Bound(t *testing.T) {
	t.Parallel()
	r := &usageBoundReader{}
	_, _, err := Usage(r, UsageTailBytes+100)
	testutil.NoError(t, err)
	testutil.Equal(t, r.bytes, UsageTailBytes)
}
func TestUsage_TruncatedTurn(t *testing.T) {
	t.Parallel()
	raw := startLine("2026-11-07T10:00:00Z", "old") + strings.Repeat("x", UsageTailBytes) + "\n" + usageTokenLine("2026-11-07T10:00:01Z", 10, 0, 1, 10, 0, 1)
	u, ok, err := Usage(strings.NewReader(raw), int64(len(raw)))
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, len(u.Turns), 0)
	testutil.Equal(t, *u.Totals.InputTokens, int64(10))
}
