// Package gitsign signs agent Git commits with the agent signing key held in
// the daemon's memory (add-agent-signing-key). It produces OpenSSH SSHSIG
// signatures (PROTOCOL.sshsig) in-process: no ssh-agent or ssh-keygen runs on
// the signing path, and the private key never touches disk.
package gitsign

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Namespace is the only SSHSIG namespace Sign accepts: Git's.
const Namespace = "git"

var (
	// ErrNamespace refuses any namespace other than "git".
	ErrNamespace = errors.New("agent signing key: only the git namespace may be signed")
	// ErrUnresolved means a key half could not be read or parsed.
	ErrUnresolved = errors.New("agent signing key unresolved")
	// ErrMismatch means the private key's public half differs from the
	// item's public key field.
	ErrMismatch = errors.New("agent signing key mismatch: private key does not match the item's public key")
)

const (
	sshsigMagic   = "SSHSIG"
	sshsigVersion = 1
	sshsigHash    = "sha512"
	armorBegin    = "-----BEGIN SSH SIGNATURE-----"
	armorEnd      = "-----END SSH SIGNATURE-----"
	armorWidth    = 70
)

// Sign returns an armored SSHSIG signature of data in namespace, made with
// signer and the sha512 hash. Only the "git" namespace is allowed.
func Sign(signer ssh.Signer, namespace string, data []byte) ([]byte, error) {
	if namespace != Namespace {
		return nil, ErrNamespace
	}
	if signer == nil {
		return nil, ErrUnresolved
	}
	digest := sha512.Sum512(data)

	// Signed blob: the magic is a raw preamble with no length prefix.
	toSign := []byte(sshsigMagic)
	toSign = appendString(toSign, []byte(namespace))
	toSign = appendString(toSign, nil) // reserved
	toSign = appendString(toSign, []byte(sshsigHash))
	toSign = appendString(toSign, digest[:])

	sig, err := signBlob(signer, toSign)
	if err != nil {
		return nil, err
	}

	blob := []byte(sshsigMagic)
	blob = binary.BigEndian.AppendUint32(blob, sshsigVersion)
	blob = appendString(blob, signer.PublicKey().Marshal())
	blob = appendString(blob, []byte(namespace))
	blob = appendString(blob, nil) // reserved
	blob = appendString(blob, []byte(sshsigHash))
	blob = appendString(blob, ssh.Marshal(sig))
	return armor(blob), nil
}

// signBlob signs with the key type's plain algorithm (ssh-ed25519 for
// Ed25519). RSA uses rsa-sha2-512: ssh-keygen rejects SHA-1 ssh-rsa SSHSIGs.
func signBlob(signer ssh.Signer, blob []byte) (*ssh.Signature, error) {
	if signer.PublicKey().Type() == ssh.KeyAlgoRSA {
		if as, ok := signer.(ssh.AlgorithmSigner); ok {
			return as.SignWithAlgorithm(rand.Reader, blob, ssh.KeyAlgoRSASHA512)
		}
	}
	return signer.Sign(rand.Reader, blob)
}

// appendString appends an SSH wire "string": uint32 length then the bytes.
func appendString(b, s []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s))) //nolint:gosec // G115: s is a key, namespace, hash name or digest — always far below 4 GiB.
	return append(b, s...)
}

// armor wraps blob as base64 at 70 columns between the SSH SIGNATURE markers.
func armor(blob []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(blob)
	var sb strings.Builder
	sb.WriteString(armorBegin)
	sb.WriteByte('\n')
	for len(enc) > armorWidth {
		sb.WriteString(enc[:armorWidth])
		sb.WriteByte('\n')
		enc = enc[armorWidth:]
	}
	if enc != "" {
		sb.WriteString(enc)
		sb.WriteByte('\n')
	}
	sb.WriteString(armorEnd)
	sb.WriteByte('\n')
	return []byte(sb.String())
}
