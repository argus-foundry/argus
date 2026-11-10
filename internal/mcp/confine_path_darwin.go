//go:build darwin

package mcp

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// descriptorPathCanonical reports that descriptorPath returns the on-disk
// (case-canonical) path of the open file itself.
const descriptorPathCanonical = true

// macPathMax is MAXPATHLEN, the buffer size F_GETPATH requires.
const macPathMax = 1024

// descriptorPath returns the path of the file f has open, in the
// filesystem's canonical case, via fcntl(F_GETPATH). Taking it from the
// descriptor (not the name the agent passed) defeats case-variant spellings
// on case-insensitive APFS and any symlink in a parent directory.
func descriptorPath(f *os.File) (string, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return "", err
	}
	buf := make([]byte, macPathMax)
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		// x/sys has no F_GETPATH wrapper; the pointer conversion stays
		// inside the Syscall call expression as unsafe.Pointer rule 4 requires.
		_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0]))) //nolint:gosec // G103: F_GETPATH fills buf; see above.
	}); err != nil {
		return "", err
	}
	if errno != 0 {
		return "", fmt.Errorf("fcntl F_GETPATH: %w", errno)
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}
