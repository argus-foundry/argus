//go:build darwin

package agent

import (
	"testing"

	"github.com/drn/argus/internal/testutil"
	"golang.org/x/sys/unix"
)

func TestOrphanBrowserSeamsDisabledUnderTest(t *testing.T) {
	t.Parallel()
	procs, err := browserSnapshot()
	testutil.NoError(t, err)
	testutil.Equal(t, len(procs), 0)
	testutil.Error(t, browserKill(1, 0))
	_, _, err = browserProcArgs(1)
	testutil.Error(t, err)
	_, _, err = browserProcByPID(1)
	testutil.Error(t, err)
	testutil.Equal(t, len(NewBrowserScanner().cache), 0)
}

func TestKinfoToBrowserProc(t *testing.T) {
	t.Parallel()
	var kp unix.KinfoProc
	kp.Proc.P_pid = 100
	kp.Eproc.Ppid = 1
	kp.Eproc.Pgid = 100
	copy(kp.Proc.P_comm[:], "Google Chrome")
	kp.Proc.P_starttime = unix.Timeval{Sec: 2, Usec: 5}
	testutil.Equal(t, kinfoToBrowserProc(&kp), BrowserProc{PID: 100, PPID: 1, PGID: 100, Comm: "Google Chrome", Start: 2_000_005})
}
