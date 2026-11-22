package gitsign

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/argus-foundry/argus/internal/testutil"
)

// decodeArmor strips the markers and checks every body line is at most 70
// columns, returning the raw SSHSIG blob.
func decodeArmor(t *testing.T, sig []byte) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(sig), "\n"), "\n")
	testutil.Equal(t, lines[0], armorBegin)
	testutil.Equal(t, lines[len(lines)-1], armorEnd)
	body := lines[1 : len(lines)-1]
	for _, l := range body {
		if len(l) > armorWidth {
			t.Fatalf("armor line is %d columns, want <= %d", len(l), armorWidth)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(body, ""))
	testutil.NoError(t, err)
	return raw
}

// readString reads one SSH wire string from b.
func readString(t *testing.T, b []byte) ([]byte, []byte) {
	t.Helper()
	if len(b) < 4 {
		t.Fatalf("short blob")
	}
	n := binary.BigEndian.Uint32(b)
	return b[4 : 4+n], b[4+n:]
}

func TestSign_BlobLayout(t *testing.T) {
	key := newTestKey(t)
	sig, err := Sign(key.signer(t), "git", []byte("payload"))
	testutil.NoError(t, err)
	raw := decodeArmor(t, sig)

	testutil.Equal(t, string(raw[:6]), "SSHSIG")
	testutil.Equal(t, binary.BigEndian.Uint32(raw[6:10]), uint32(1))
	rest := raw[10:]
	var pub, ns, reserved, hash, sigBlob []byte
	pub, rest = readString(t, rest)
	ns, rest = readString(t, rest)
	reserved, rest = readString(t, rest)
	hash, rest = readString(t, rest)
	sigBlob, rest = readString(t, rest)
	testutil.Equal(t, len(rest), 0)
	testutil.DeepEqual(t, pub, key.ssh.Marshal())
	testutil.Equal(t, string(ns), "git")
	testutil.Equal(t, len(reserved), 0)
	testutil.Equal(t, string(hash), "sha512")

	var s ssh.Signature
	testutil.NoError(t, ssh.Unmarshal(sigBlob, &s))
	// Ed25519 signs with the plain ssh-ed25519 algorithm.
	testutil.Equal(t, s.Format, ssh.KeyAlgoED25519)
}

func TestSign_NilSigner(t *testing.T) {
	_, err := Sign(nil, "git", []byte("x"))
	testutil.ErrorIs(t, err, ErrUnresolved)
}

func TestSign_RSAUsesSHA512AndVerifies(t *testing.T) {
	if testing.Short() {
		t.Skip("RSA key generation is slow")
	}
	sk, err := rsa.GenerateKey(rand.Reader, 2048)
	testutil.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(sk)
	testutil.NoError(t, err)
	data := []byte("rsa payload\n")
	sig, err := Sign(signer, "git", data)
	testutil.NoError(t, err)

	raw := decodeArmor(t, sig)
	rest := raw[10:]
	for i := 0; i < 4; i++ {
		_, rest = readString(t, rest)
	}
	sigBlob, _ := readString(t, rest)
	var s ssh.Signature
	testutil.NoError(t, ssh.Unmarshal(sigBlob, &s))
	testutil.Equal(t, s.Format, ssh.KeyAlgoRSASHA512)

	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return
	}
	dir := t.TempDir()
	allowed := filepath.Join(dir, "allowed_signers")
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	testutil.NoError(t, os.WriteFile(allowed, []byte("agent@argus "+pub+"\n"), 0o600))
	sigPath := filepath.Join(dir, "data.sig")
	testutil.NoError(t, os.WriteFile(sigPath, sig, 0o600))
	cmd := exec.Command(keygen, "-Y", "verify", "-f", allowed, "-I", "agent@argus", "-n", "git", "-s", sigPath)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y verify: %v: %s", err, out)
	}
}

func TestKeyHolder_ParseAndConfigErrors(t *testing.T) {
	key := newTestKey(t)
	for _, tc := range []struct {
		name    string
		ref     string
		resolve Resolver
	}{
		{"blank ref", "  ", newStubResolver(nil).resolve},
		{"nil resolver", testRef, nil},
		{"private not a key", testRef, newStubResolver(map[string]string{
			PublicKeyRef(testRef): key.pub, PrivateKeyRef(testRef): "not a key",
		}).resolve},
		{"public not a key", testRef, newStubResolver(map[string]string{
			PublicKeyRef(testRef): "garbage", PrivateKeyRef(testRef): key.priv,
		}).resolve},
		{"private blank", testRef, newStubResolver(map[string]string{
			PublicKeyRef(testRef): key.pub, PrivateKeyRef(testRef): "  ",
		}).resolve},
		{"pkcs8 form", testRef, newStubResolver(map[string]string{
			PublicKeyRef(testRef):  key.pub,
			PrivateKeyRef(testRef): string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})),
		}).resolve},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewKeyHolder(tc.resolve, tc.ref).Signer()
			testutil.ErrorIs(t, err, ErrUnresolved)
			testutil.Nil(t, s)
			testutil.False(t, strings.Contains(err.Error(), "PRIVATE KEY"))
			testutil.False(t, strings.Contains(err.Error(), key.pub))
			testutil.False(t, strings.Contains(err.Error(), "garbage"))
		})
	}
}
