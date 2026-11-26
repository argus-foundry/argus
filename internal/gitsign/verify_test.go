package gitsign

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestVerify(t *testing.T) {
	key, other := newTestKey(t), newTestKey(t)
	data := []byte("tree 4b825dc6\nauthor A <a@example.com> 0 +0000\n\nmsg\n")
	sig, err := Sign(key.signer(t), "git", data)
	testutil.NoError(t, err)
	otherSig, err := Sign(other.signer(t), "git", data)
	testutil.NoError(t, err)

	testutil.NoError(t, Verify(key.ssh, "git", data, sig))
	for _, tc := range []struct {
		name      string
		namespace string
		data      []byte
		sig       []byte
	}{
		{"different key", "git", data, otherSig},
		{"different namespace", "file", data, sig},
		{"tampered data", "git", append([]byte("x"), data...), sig},
		{"not armored", "git", data, []byte("garbage")},
		{"bad base64", "git", data, []byte("-----BEGIN SSH SIGNATURE-----\n!!!!\n-----END SSH SIGNATURE-----\n")},
		{"truncated blob", "git", data, []byte("-----BEGIN SSH SIGNATURE-----\nU1NIU0lH\n-----END SSH SIGNATURE-----\n")},
		{"empty", "git", data, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Verify(key.ssh, tc.namespace, tc.data, tc.sig)
			testutil.ErrorIs(t, err, ErrBadSignature)
			testutil.False(t, strings.Contains(err.Error(), key.pub))
		})
	}
}

// Overlapping BEGIN/END markers must be a named error, not a slice panic.
func TestVerify_OverlappingArmorMarkers(t *testing.T) {
	key := newTestKey(t)
	armored := []byte("-----BEGIN SSH SIGNATURE-----END SSH SIGNATURE-----")
	testutil.Equal(t, len(armored), 51)
	err := Verify(key.ssh, "git", []byte("data"), armored)
	testutil.ErrorIs(t, err, ErrBadSignature)
}

// sshsigWithAlgo builds an armored SSHSIG whose inner signature uses algo.
func sshsigWithAlgo(t *testing.T, s ssh.AlgorithmSigner, algo string, data []byte) []byte {
	t.Helper()
	digest := sha512.Sum512(data)
	toSign := []byte(sshsigMagic)
	toSign = appendString(toSign, []byte("git"))
	toSign = appendString(toSign, nil)
	toSign = appendString(toSign, []byte(sshsigHash))
	toSign = appendString(toSign, digest[:])
	sig, err := s.SignWithAlgorithm(rand.Reader, toSign, algo)
	testutil.NoError(t, err)
	blob := []byte(sshsigMagic)
	blob = binary.BigEndian.AppendUint32(blob, sshsigVersion)
	blob = appendString(blob, s.PublicKey().Marshal())
	blob = appendString(blob, []byte("git"))
	blob = appendString(blob, nil)
	blob = appendString(blob, []byte(sshsigHash))
	blob = appendString(blob, ssh.Marshal(sig))
	return armor(blob)
}

// PROTOCOL.sshsig forbids SHA-1 "ssh-rsa" signatures; rsa-sha2-512 is fine.
func TestVerify_RSAAlgorithms(t *testing.T) {
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	testutil.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rk)
	testutil.NoError(t, err)
	as := signer.(ssh.AlgorithmSigner)
	data := []byte("commit payload")

	testutil.NoError(t, Verify(signer.PublicKey(), "git", data, sshsigWithAlgo(t, as, ssh.KeyAlgoRSASHA512, data)))
	err = Verify(signer.PublicKey(), "git", data, sshsigWithAlgo(t, as, ssh.KeyAlgoRSA, data))
	testutil.ErrorIs(t, err, ErrBadSignature)
	testutil.Contains(t, err.Error(), "SHA-1")
}
