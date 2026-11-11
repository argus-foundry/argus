package inject

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// Claude 2.1.317 uses proper-lockfile's mkdir(<config>.lock) protocol,
// with a 10s stale threshold and 5s heartbeat. Refresh more often and never
// steal an existing directory: a timeout is safer than losing another write.
const claudeLockWait = 500 * time.Millisecond

type claudeConfigLock struct {
	path string
	dir  *os.File
	info os.FileInfo
	lost atomic.Bool
	stop chan struct{}
	done chan struct{}
}

func acquireClaudeConfigLock(path string, wait, refresh time.Duration) (*claudeConfigLock, error) {
	path += ".lock"
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("trust claude: acquire config lock: %w", err)
		}
		select {
		case <-deadline.C:
			return nil, fmt.Errorf("trust claude: config lock busy after %s", wait)
		case <-time.After(10 * time.Millisecond):
		}
	}
	dir, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("trust claude: open config lock: %w", err)
	}
	info, err := dir.Stat()
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	l := &claudeConfigLock{path: path, dir: dir, info: info, stop: make(chan struct{}), done: make(chan struct{})}
	go l.keepAlive(refresh)
	return l, nil
}

func (l *claudeConfigLock) check() error {
	info, err := os.Lstat(l.path)
	if err != nil || l.lost.Load() || !os.SameFile(l.info, info) {
		return fmt.Errorf("trust claude: config lock ownership lost")
	}
	return nil
}

func (l *claudeConfigLock) keepAlive(refresh time.Duration) {
	defer close(l.done)
	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			if l.refresh() != nil {
				l.lost.Store(true)
				return
			}
		}
	}
}

func (l *claudeConfigLock) refresh() error {
	if err := l.check(); err != nil {
		return err
	}
	// Use current wall time, not a queued ticker timestamp after host suspend.
	stamp := unix.NsecToTimeval(time.Now().UnixNano())
	fd := int(l.dir.Fd()) //nolint:gosec // Open OS descriptors fit int; close waits for this goroutine.
	// Updating the open descriptor never touches a successor's lock.
	if err := unix.Futimes(fd, []unix.Timeval{stamp, stamp}); err != nil {
		return err
	}
	return l.check()
}

func (l *claudeConfigLock) close() error {
	close(l.stop)
	<-l.done
	err := l.check()
	if err == nil {
		err = os.Remove(l.path)
	}
	return errors.Join(err, l.dir.Close())
}
