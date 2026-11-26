package events

import (
	"sync"
	"sync/atomic"

	"github.com/argus-foundry/argus/internal/model"
)

// listenSet is an immutable snapshot of the subscribed listeners. Emit loads
// it once and calls that slice, so Subscribe and Unsubscribe never mutate a
// slice Emit is ranging over.
type listenSet struct {
	fns []func(model.Event)
}

type listener struct {
	id int64
	fn func(model.Event)
}

var (
	listenMu   sync.Mutex
	listenSeq  int64
	listenReg  []listener
	listenSnap atomic.Pointer[listenSet]
)

// Subscribe registers fn to receive every subsequent Emit. The returned
// function removes it. A nil fn subscribes nothing and returns a no-op
// unsubscribe. Listeners are invoked synchronously on the emitter's
// goroutine after the sink; they must not block (enqueue and return).
func Subscribe(fn func(model.Event)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	listenMu.Lock()
	defer listenMu.Unlock()
	listenSeq++
	id := listenSeq
	listenReg = append(listenReg, listener{id: id, fn: fn})
	publishListenersLocked()
	return func() {
		listenMu.Lock()
		defer listenMu.Unlock()
		next := listenReg[:0:0]
		for _, l := range listenReg {
			if l.id != id {
				next = append(next, l)
			}
		}
		listenReg = next
		publishListenersLocked()
	}
}

func publishListenersLocked() {
	fns := make([]func(model.Event), len(listenReg))
	for i, l := range listenReg {
		fns[i] = l.fn
	}
	listenSnap.Store(&listenSet{fns: fns})
}

// listenerCount reports how many listeners are registered. Tests use it to
// prove unsubscribe drained the set.
func listenerCount() int {
	listenMu.Lock()
	defer listenMu.Unlock()
	return len(listenReg)
}
