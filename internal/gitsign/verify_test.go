package gitsign

import (
	"strings"
	"testing"

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
