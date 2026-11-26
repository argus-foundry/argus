package gitsign

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ErrBadSignature means an armored SSHSIG does not verify for the expected
// public key, namespace and data.
var ErrBadSignature = errors.New("SSH signature does not verify")

// Verify checks that armored is an SSHSIG over data in namespace made by pub
// (PROTOCOL.sshsig; the inverse of Sign). Every failure wraps ErrBadSignature
// with a fixed cause that never echoes key material.
func Verify(pub ssh.PublicKey, namespace string, data, armored []byte) error {
	fail := func(cause string) error { return fmt.Errorf("%w: %s", ErrBadSignature, cause) }
	blob, err := unarmor(armored)
	if err != nil {
		return fail(err.Error())
	}
	if !bytes.HasPrefix(blob, []byte(sshsigMagic)) {
		return fail("missing SSHSIG magic")
	}
	r := blob[len(sshsigMagic):]
	if len(r) < 4 || binary.BigEndian.Uint32(r) != sshsigVersion {
		return fail("unsupported SSHSIG version")
	}
	r = r[4:]
	var fields [5][]byte // public key, namespace, reserved, hash, signature
	for i := range fields {
		var ok bool
		if fields[i], r, ok = readWireString(r); !ok {
			return fail("truncated SSHSIG blob")
		}
	}
	if len(r) != 0 {
		return fail("trailing bytes after SSHSIG blob")
	}
	if !bytes.Equal(fields[0], pub.Marshal()) {
		return fail("signed by a different key")
	}
	if string(fields[1]) != namespace {
		return fail("wrong namespace")
	}
	var digest []byte
	switch string(fields[3]) {
	case "sha512":
		d := sha512.Sum512(data)
		digest = d[:]
	case "sha256":
		d := sha256.Sum256(data)
		digest = d[:]
	default:
		return fail("unsupported hash algorithm")
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(fields[4], &sig); err != nil {
		return fail("malformed signature")
	}
	// PROTOCOL.sshsig: RSA signatures must use rsa-sha2-*, never SHA-1 ssh-rsa.
	if sig.Format == ssh.KeyAlgoRSA {
		return fail("SHA-1 ssh-rsa signatures are not accepted")
	}
	toSign := []byte(sshsigMagic)
	toSign = appendString(toSign, fields[1])
	toSign = appendString(toSign, fields[2])
	toSign = appendString(toSign, fields[3])
	toSign = appendString(toSign, digest)
	if err := pub.Verify(toSign, &sig); err != nil {
		return fail("signature does not match the data")
	}
	return nil
}

// unarmor decodes the base64 body between the SSH SIGNATURE markers.
func unarmor(armored []byte) ([]byte, error) {
	s := strings.TrimSpace(string(armored))
	// The length check stops overlapping markers from slicing out of range.
	if len(s) < len(armorBegin)+len(armorEnd) || !strings.HasPrefix(s, armorBegin) || !strings.HasSuffix(s, armorEnd) {
		return nil, errors.New("not an armored SSH signature")
	}
	body := strings.Join(strings.Fields(s[len(armorBegin):len(s)-len(armorEnd)]), "")
	blob, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("signature body is not base64")
	}
	return blob, nil
}

// readString reads one SSH wire string (uint32 length then bytes).
func readWireString(b []byte) (s, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, b, false
	}
	n := binary.BigEndian.Uint32(b)
	if int64(n) > int64(len(b)-4) {
		return nil, b, false
	}
	return b[4 : 4+n], b[4+n:], true
}
