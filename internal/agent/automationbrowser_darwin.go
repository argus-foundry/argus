//go:build darwin

package agent

import (
	"errors"

	"golang.org/x/sys/unix"
)

// browserSnapshot reads the whole process table in one sysctl. Disabled under
// `go test` so a test never sees real processes.
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
