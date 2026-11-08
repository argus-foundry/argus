//go:build !darwin

package agent

import (
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestOrphanBrowserStubsAreInert(t *testing.T) {
	t.Parallel()
	procs, err := browserSnapshot()
	testutil.NoError(t, err)
	testutil.Equal(t, len(procs), 0)
	_, ok, err := browserProcByPID(1)
	testutil.NoError(t, err)
	testutil.False(t, ok)
	argv, env, err := browserProcArgs(1)
	testutil.NoError(t, err)
	testutil.Equal(t, len(argv)+len(env), 0)
	testutil.Error(t, browserKill(1, 0))
	got, err := NewBrowserScanner().Scan()
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
}
