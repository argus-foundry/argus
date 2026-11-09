package api

import (
	"context"
	"sync"
)

// taskLocker serializes work per task ID. The zero value is ready to use.
// Entries are refcounted and dropped when idle so a long-lived daemon does not
// accumulate one mutex per task ever touched.
type taskLocker struct {
	mu sync.Mutex
	m  map[string]*taskLockEntry
}

type taskLockEntry struct {
	gate chan struct{}
	refs int
}

// Lock blocks until the caller holds the per-task lock and returns its release
// func. Callers must not re-enter Lock for the same ID while holding it.
func (l *taskLocker) Lock(id string) (unlock func()) {
	unlock, _ = l.LockContext(context.Background(), id)
	return unlock
}

// LockContext acquires the same task lock until canceled.
func (l *taskLocker) LockContext(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.m == nil {
		l.m = make(map[string]*taskLockEntry)
	}
	e := l.m[id]
	if e == nil {
		e = &taskLockEntry{gate: make(chan struct{}, 1)}
		l.m[id] = e
	}
	e.refs++
	l.mu.Unlock()

	releaseRef := func() {
		l.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(l.m, id)
		}
		l.mu.Unlock()
	}
	select {
	case e.gate <- struct{}{}:
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			<-e.gate
			releaseRef()
		})
	}, nil
}

func (l *taskLocker) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}
