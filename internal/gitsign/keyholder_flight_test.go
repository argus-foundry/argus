package gitsign

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/testutil"
)

// waiters reports how many callers are parked on the in-flight attempt.
func (h *KeyHolder) waiters() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight == nil {
		return 0
	}
	return h.inflight.waiters
}

// Concurrent callers during an outage share one in-flight attempt instead of
// each re-running the (slow) resolve behind the mutex; a later call retries.
func TestKeyHolder_SingleFlight(t *testing.T) {
	const n = 8
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var privCalls, pubCalls atomic.Int32
	resolve := func(source string) (string, bool) {
		switch source {
		case PrivateKeyRef(testRef):
			if privCalls.Add(1) == 1 {
				started <- struct{}{}
				<-release
			}
		case PublicKeyRef(testRef):
			pubCalls.Add(1)
		}
		return "", false // 1Password outage: nothing resolves
	}
	h := NewKeyHolder(resolve, testRef)

	errs := make([]error, n)
	signers := make([]ssh.Signer, n)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		signers[0], errs[0] = h.Signer()
	}()
	<-started
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			signers[i], errs[i] = h.Signer()
		}(i)
	}
	deadline := time.Now().Add(10 * time.Second)
	for h.waiters() != n-1 {
		if time.Now().After(deadline) {
			t.Fatalf("waiters = %d, want %d", h.waiters(), n-1)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()

	testutil.Equal(t, privCalls.Load(), int32(1))
	for i := range n {
		testutil.ErrorIs(t, errs[i], ErrUnresolved)
		testutil.Nil(t, signers[i])
	}

	// The failure is not memoized: the next call starts a new attempt.
	_, err := h.Signer()
	testutil.ErrorIs(t, err, ErrUnresolved)
	testutil.Equal(t, privCalls.Load(), int32(2))
	testutil.Equal(t, h.waiters(), 0)
}

func TestKeyHolder_PassphraseProtected(t *testing.T) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(sk, "", []byte("hunter2"))
	testutil.NoError(t, err)
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	res := newStubResolver(map[string]string{
		PublicKeyRef(testRef):  pub,
		PrivateKeyRef(testRef): string(pem.EncodeToMemory(block)),
	})
	s, err := NewKeyHolder(res.resolve, testRef).Signer()
	testutil.ErrorIs(t, err, ErrUnresolved)
	testutil.Nil(t, s)
	testutil.Contains(t, err.Error(), "is passphrase-protected")
	testutil.False(t, strings.Contains(err.Error(), "PRIVATE KEY"))
	testutil.False(t, strings.Contains(err.Error(), pub))
}
