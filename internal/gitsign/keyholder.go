package gitsign

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// policyAgentKey mirrors config.GitSigningAgentKey; kept as a literal so this
// package does not import internal/config.
const policyAgentKey = "agent-key"

// Resolver resolves a secret reference (an op:// URI) to its value. The bool
// is false when the reference cannot be resolved.
type Resolver func(source string) (string, bool)

// PublicKeyRef is the 1Password field reference for the item's public key.
func PublicKeyRef(ref string) string { return ref + "/public key" }

// PrivateKeyRef is the 1Password field reference for the item's private key
// in OpenSSH format (the default PKCS#8 form does not parse as an SSH key).
func PrivateKeyRef(ref string) string { return ref + "/private key?ssh-format=openssh" }

// KeyHolder resolves the agent signing key on first use and keeps the parsed
// signer in memory for the life of the process. Only success is memoized: a
// failed resolve is retried on the next call, so a 1Password recovery needs
// no restart.
type KeyHolder struct {
	resolve Resolver
	ref     string

	mu     sync.Mutex
	signer ssh.Signer
}

// NewKeyHolder returns a holder that resolves ref's key halves through resolve.
func NewKeyHolder(resolve Resolver, ref string) *KeyHolder {
	return &KeyHolder{resolve: resolve, ref: ref}
}

// Signer returns the memoized signer, resolving it first if needed. Errors
// wrap ErrUnresolved or ErrMismatch with a cause that never includes key
// material.
func (h *KeyHolder) Signer() (ssh.Signer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.signer != nil {
		return h.signer, nil
	}
	s, err := loadSigner(h.resolve, h.ref)
	if err != nil {
		return nil, err
	}
	h.signer = s
	return s, nil
}

// loadSigner resolves and parses both halves and checks they match.
func loadSigner(resolve Resolver, ref string) (ssh.Signer, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("%w: git.agent_signing_key is not set", ErrUnresolved)
	}
	if resolve == nil {
		return nil, fmt.Errorf("%w: no secret resolver configured", ErrUnresolved)
	}
	privRef, pubRef := PrivateKeyRef(ref), PublicKeyRef(ref)
	priv, ok := resolve(privRef)
	if !ok || strings.TrimSpace(priv) == "" {
		return nil, fmt.Errorf("%w: %s did not resolve", ErrUnresolved, privRef)
	}
	pub, ok := resolve(pubRef)
	if !ok || strings.TrimSpace(pub) == "" {
		return nil, fmt.Errorf("%w: %s did not resolve", ErrUnresolved, pubRef)
	}
	// Parse errors are reported by kind only: the x/crypto messages never
	// echo the input, but the cause here is fixed text to be certain.
	signer, err := ssh.ParsePrivateKey([]byte(priv))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not an OpenSSH private key", ErrUnresolved, privRef)
	}
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not an SSH public key", ErrUnresolved, pubRef)
	}
	if !bytes.Equal(signer.PublicKey().Marshal(), want.Marshal()) {
		return nil, fmt.Errorf("%w (%s)", ErrMismatch, ref)
	}
	return signer, nil
}

// Status is the agent signing key's state for `argus doctor`.
type Status struct {
	State string // "OFF" | "OK" | "BROKEN"
	Cause string
}

// Check reports the key's state: OFF when policy is not "agent-key" (no
// resolve happens), OK when both halves resolve and match, else BROKEN with
// the named cause.
func Check(policy, ref string, resolve Resolver) Status {
	if policy != policyAgentKey {
		return Status{State: "OFF"}
	}
	if _, err := loadSigner(resolve, ref); err != nil {
		return Status{State: "BROKEN", Cause: err.Error()}
	}
	return Status{State: "OK"}
}
