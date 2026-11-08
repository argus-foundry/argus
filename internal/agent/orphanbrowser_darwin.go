//go:build darwin

package agent

import (
	"errors"

	"golang.org/x/sys/unix"
)

// browserSnapshot reads the whole process table in one sysctl. Disabled under
// `go test` so a test never sees (or acts on) real processes.
func browserSnapshot() ([]BrowserProc, error) {
	if isTestBinary() {
		return nil, nil
	}
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make([]BrowserProc, 0, len(kps))
	for i := range kps {
		out = append(out, kinfoToBrowserProc(&kps[i]))
	}
	return out, nil
}

func kinfoToBrowserProc(kp *unix.KinfoProc) BrowserProc {
	st := kp.Proc.P_starttime
	return BrowserProc{
		PID:   kp.Proc.P_pid,
		PPID:  kp.Eproc.Ppid,
		PGID:  kp.Eproc.Pgid,
		Comm:  unix.ByteSliceToString(kp.Proc.P_comm[:]),
		Start: int64(st.Sec)*1_000_000 + int64(st.Usec),
	}
}

// browserProcByPID re-reads one PID's row. ok=false when it no longer exists.
func browserProcByPID(pid int32) (BrowserProc, bool, error) {
	if isTestBinary() {
		return BrowserProc{}, false, errors.New("process lookup disabled in test binary")
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if err != nil {
		// A dead pid makes the sysctl return zero bytes, which x/sys reports
		// as EIO.
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EIO) {
			return BrowserProc{}, false, nil
		}
		return BrowserProc{}, false, err
	}
	if kp.Proc.P_pid != pid {
		return BrowserProc{}, false, nil
	}
	return kinfoToBrowserProc(kp), true, nil
}

// browserProcArgs reads one process's argv and environment (same-user
// processes only; that is all an agent can launch).
func browserProcArgs(pid int32) ([]string, []string, error) {
	if isTestBinary() {
		return nil, nil, errors.New("procargs disabled in test binary")
	}
	buf, err := unix.SysctlRaw("kern.procargs2", int(pid))
	if err != nil {
		return nil, nil, err
	}
	return parseProcArgs2(buf)
}
