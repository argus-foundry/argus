package api

import "sync"

// taskLocker serializes work per task ID. The zero value is ready to use.
// Entries are refcounted and dropped when idle so a long-lived daemon does not
// accumulate one mutex per task ever touched.
type taskLocker struct {
	mu sync.Mutex
	m  map[string]*taskLockEntry
}

type taskLockEntry struct {
	mu   sync.Mutex
	refs int
}

// Lock blocks until the caller holds the per-task lock and returns its release
// func. Callers must not re-enter Lock for the same ID while holding it.
func (l *taskLocker) Lock(id string) (unlock func()) {
	l.mu.Lock()
	if l.m == nil {
		l.m = make(map[string]*taskLockEntry)
	}
	e := l.m[id]
	if e == nil {
		e = &taskLockEntry{}
		l.m[id] = e
	}
	e.refs++
	l.mu.Unlock()

	e.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Unlock()
			l.mu.Lock()
			e.refs--
			if e.refs == 0 {
				delete(l.m, id)
			}
			l.mu.Unlock()
		})
	}
}

func (l *taskLocker) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}
