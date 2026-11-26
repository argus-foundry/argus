package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// LockSessionMutation waits for the task's current lifecycle operation to
// finish. Use it from background work that can be canceled.
func (d *DB) LockSessionMutation(ctx context.Context, taskID string) (func(), error) {
	retry := time.NewTicker(10 * time.Millisecond)
	defer retry.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, acquired, err := d.TryLockSessionMutation(taskID)
		if err != nil || acquired {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-retry.C:
		}
	}
}

// TryLockSessionMutation acquires exclusive ownership of a task's session
// lifecycle across handles and processes that open this database. A competing
// owner returns (nil, false, nil) without waiting. Call unlock after all
// session and database changes for the operation have finished.
func (d *DB) TryLockSessionMutation(taskID string) (unlock func(), acquired bool, err error) {
	if taskID == "" {
		return nil, false, errors.New("session mutation requires a task ID")
	}
	if d.sessionLockDir == "" {
		if _, held := d.sessionLocks.LoadOrStore(taskID, struct{}{}); held {
			return nil, false, nil
		}
		var release sync.Once
		return func() { release.Do(func() { d.sessionLocks.Delete(taskID) }) }, true, nil
	}

	if err := os.MkdirAll(d.sessionLockDir, 0700); err != nil {
		return nil, false, fmt.Errorf("create session lock directory: %w", err)
	}
	hash := sha256.Sum256([]byte(taskID))
	path := filepath.Join(d.sessionLockDir, hex.EncodeToString(hash[:])+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, false, fmt.Errorf("open session lock file: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // Kernel file descriptors fit in int.
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err == nil {
		// Closing the descriptor releases the lock. Keep the inode in place:
		// unlinking could let two holders lock different files for this task.
		return func() { _ = file.Close() }, true, nil
	}
	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("acquire session lock: %w", err)
}
