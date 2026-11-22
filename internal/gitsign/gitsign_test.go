package gitsign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/testutil"
)

const testRef = "op://claude/Argus agent signing key"

// testKey is a synthetic Ed25519 key generated in memory for one test.
type testKey struct {
	pub  string // authorized_keys form, as 1Password's "public key" field returns it
	priv string // OpenSSH PEM, as "private key?ssh-format=openssh" returns it
	ssh  ssh.PublicKey
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	testutil.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pk)
	testutil.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(sk, "")
	testutil.NoError(t, err)
	return testKey{
		pub:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
		priv: string(pem.EncodeToMemory(block)),
		ssh:  sshPub,
	}
}

func (k testKey) signer(t *testing.T) ssh.Signer {
	t.Helper()
	s, err := ssh.ParsePrivateKey([]byte(k.priv))
	testutil.NoError(t, err)
	return s
}

// stubResolver answers from a map and counts calls per source.
type stubResolver struct {
	values map[string]string
	calls  map[string]*atomic.Int32
}

func newStubResolver(values map[string]string) *stubResolver {
	return &stubResolver{values: values, calls: map[string]*atomic.Int32{}}
}

func (s *stubResolver) resolve(source string) (string, bool) {
	if s.calls[source] == nil {
		s.calls[source] = &atomic.Int32{}
	}
	s.calls[source].Add(1)
	v, ok := s.values[source]
	return v, ok
}

func (s *stubResolver) count(source string) int32 {
	if c := s.calls[source]; c != nil {
		return c.Load()
	}
	return 0
}

// mustSigner fails the test (instead of panicking later on a nil signer)
// when the holder cannot produce one.
func mustSigner(t *testing.T, h *KeyHolder) ssh.Signer {
	t.Helper()
	s, err := h.Signer()
	if err != nil || s == nil {
		t.Fatalf("Signer() = %v, %v; want a signer", s, err)
	}
	return s
}

func TestKeyRefs(t *testing.T) {
	testutil.Equal(t, PublicKeyRef(testRef), testRef+"/public key")
	testutil.Equal(t, PrivateKeyRef(testRef), testRef+"/private key?ssh-format=openssh")
}

func TestSign_VerifiesWithSSHKeygen(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not on PATH")
	}
	key := newTestKey(t)
	data := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\nauthor A <a@example.com> 0 +0000\n\nmsg\n")
	sig, err := Sign(key.signer(t), "git", data)
	testutil.NoError(t, err)
	testutil.Equal(t, strings.HasPrefix(string(sig), "-----BEGIN SSH SIGNATURE-----\n"), true)
	testutil.Contains(t, string(sig), "-----END SSH SIGNATURE-----")

	dir := t.TempDir()
	// Public material only: the allowed_signers file holds the public key.
	allowed := filepath.Join(dir, "allowed_signers")
	testutil.NoError(t, os.WriteFile(allowed, []byte("agent@argus namespaces=\"git\" "+key.pub+"\n"), 0o600))
	sigPath := filepath.Join(dir, "data.sig")
	testutil.NoError(t, os.WriteFile(sigPath, sig, 0o600))

	verify := func(ns string, payload []byte) ([]byte, error) {
		cmd := exec.Command(keygen, "-Y", "verify", "-f", allowed, "-I", "agent@argus", "-n", ns, "-s", sigPath)
		cmd.Stdin = bytes.NewReader(payload)
		return cmd.CombinedOutput()
	}
	out, err := verify("git", data)
	if err != nil {
		t.Fatalf("ssh-keygen -Y verify: %v: %s", err, out)
	}
	testutil.Contains(t, string(out), "Good \"git\" signature")

	// The signature binds both the data and the namespace.
	_, err = verify("git", append([]byte("tampered "), data...))
	testutil.Error(t, err)
	_, err = verify("file", data)
	testutil.Error(t, err)
}

func TestSign_RefusesOtherNamespaces(t *testing.T) {
	signer := newTestKey(t).signer(t)
	for _, ns := range []string{"", "file", "Git", "git ", "email"} {
		t.Run("ns="+ns, func(t *testing.T) {
			sig, err := Sign(signer, ns, []byte("data"))
			testutil.ErrorIs(t, err, ErrNamespace)
			testutil.Equal(t, len(sig), 0)
		})
	}
}

func TestKeyHolder_Signer(t *testing.T) {
	key := newTestKey(t)
	res := newStubResolver(map[string]string{
		PublicKeyRef(testRef):  key.pub,
		PrivateKeyRef(testRef): key.priv,
	})
	h := NewKeyHolder(res.resolve, testRef)
	s := mustSigner(t, h)
	testutil.DeepEqual(t, s.PublicKey().Marshal(), key.ssh.Marshal())

	// Success is memoized for the life of the holder.
	s2 := mustSigner(t, h)
	testutil.DeepEqual(t, s2.PublicKey().Marshal(), key.ssh.Marshal())
	testutil.Equal(t, res.count(PrivateKeyRef(testRef)), int32(1))
	testutil.Equal(t, res.count(PublicKeyRef(testRef)), int32(1))
}

func TestKeyHolder_Errors(t *testing.T) {
	key, other := newTestKey(t), newTestKey(t)
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   error
	}{
		{"private unresolved", map[string]string{PublicKeyRef(testRef): key.pub}, ErrUnresolved},
		{"public unresolved", map[string]string{PrivateKeyRef(testRef): key.priv}, ErrUnresolved},
		{"nothing resolves", map[string]string{}, ErrUnresolved},
		{"mismatched halves", map[string]string{PublicKeyRef(testRef): other.pub, PrivateKeyRef(testRef): key.priv}, ErrMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewKeyHolder(newStubResolver(tc.values).resolve, testRef)
			s, err := h.Signer()
			testutil.ErrorIs(t, err, tc.want)
			testutil.Nil(t, s)
			if err == nil {
				return
			}
			// Named cause, never key material.
			testutil.False(t, strings.Contains(err.Error(), "PRIVATE KEY"))
			testutil.False(t, strings.Contains(err.Error(), key.pub))
			testutil.False(t, strings.Contains(err.Error(), other.pub))
		})
	}
}

func TestKeyHolder_RetriesAfterFailedResolve(t *testing.T) {
	key := newTestKey(t)
	res := newStubResolver(map[string]string{PublicKeyRef(testRef): key.pub})
	h := NewKeyHolder(res.resolve, testRef)

	_, err := h.Signer()
	testutil.ErrorIs(t, err, ErrUnresolved)

	// 1Password recovers: the next call re-resolves, no restart needed.
	res.values[PrivateKeyRef(testRef)] = key.priv
	s := mustSigner(t, h)
	testutil.DeepEqual(t, s.PublicKey().Marshal(), key.ssh.Marshal())
	testutil.Equal(t, res.count(PrivateKeyRef(testRef)), int32(2))
}

func TestCheck(t *testing.T) {
	key, other := newTestKey(t), newTestKey(t)
	good := map[string]string{PublicKeyRef(testRef): key.pub, PrivateKeyRef(testRef): key.priv}
	for _, tc := range []struct {
		name, policy, ref string
		values            map[string]string
		wantState         string
		wantCause         bool
	}{
		{"policy absent", "", testRef, good, "OFF", false},
		{"inherit", "inherit", testRef, good, "OFF", false},
		{"unsigned", "unsigned", testRef, good, "OFF", false},
		{"healthy", "agent-key", testRef, good, "OK", false},
		{"unresolved", "agent-key", testRef, map[string]string{}, "BROKEN", true},
		{"mismatch", "agent-key", testRef, map[string]string{PublicKeyRef(testRef): other.pub, PrivateKeyRef(testRef): key.priv}, "BROKEN", true},
		{"no ref", "agent-key", "", good, "BROKEN", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := newStubResolver(tc.values)
			st := Check(tc.policy, tc.ref, res.resolve)
			testutil.Equal(t, st.State, tc.wantState)
			testutil.Equal(t, st.Cause != "", tc.wantCause)
			for _, secret := range []string{key.pub, other.pub, "PRIVATE KEY"} {
				testutil.False(t, strings.Contains(st.Cause, secret))
			}
			if tc.wantState == "OFF" {
				// OFF never touches 1Password.
				testutil.Equal(t, res.count(PrivateKeyRef(testRef))+res.count(PublicKeyRef(testRef)), int32(0))
			}
		})
	}
}

// Sentinel errors stay distinct so callers can branch on them.
func TestSentinelsDistinct(t *testing.T) {
	testutil.False(t, errors.Is(ErrUnresolved, ErrMismatch))
	testutil.False(t, errors.Is(ErrNamespace, ErrUnresolved))
	testutil.False(t, errors.Is(ErrMismatch, ErrNamespace))
}
