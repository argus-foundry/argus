package client

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCheckAccountSupported(t *testing.T) {
	testutil.True(t, accountProtocolVersion <= daemon.ProtocolVersion)
	cases := []struct {
		name    string
		peer    int
		account string
		wantErr bool
	}{
		{"never handshaken", 0, "personal", false},
		{"old supervisor default account", 7, "", false},
		{"old supervisor explicit default", 7, "default", false},
		{"old supervisor explicit account", 7, "personal", true},
		{"current supervisor explicit account", accountProtocolVersion, "personal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{peerProtocol: tc.peer}
			err := c.checkAccountSupported(&model.Task{ID: "t", Account: tc.account})
			if tc.wantErr {
				testutil.Error(t, err)
				testutil.Contains(t, err.Error(), "restart the supervisor")
				return
			}
			testutil.NoError(t, err)
		})
	}
}

func TestStart_RefusesAccountOnOldSupervisor(t *testing.T) {
	c := &Client{peerProtocol: 7}
	_, err := c.Start(&model.Task{ID: "t", Account: "personal"}, configZero(), 24, 80, false)
	testutil.Error(t, err)
	testutil.Error(t, c.KickRerender(&model.Task{ID: "t", Account: "personal"}, configZero(), 24, 80))
	testutil.Error(t, c.Recycle(&model.Task{ID: "t", Account: "personal"}, configZero(), 24, 80))
}

func configZero() config.Config { return config.Config{} }

func TestCheckResidentSupported(t *testing.T) {
	testutil.Equal(t, residentProtocolVersion, 13)
	testutil.True(t, residentProtocolVersion <= daemon.ProtocolVersion)
	cases := []struct {
		name    string
		peer    int
		kind    string
		wantErr bool
	}{
		{"never handshaken resident", 0, model.KindResident, false},
		{"old supervisor resident", 12, model.KindResident, true},
		{"current supervisor resident", residentProtocolVersion, model.KindResident, false},
		{"old supervisor plain", 12, "", false},
		{"current supervisor plain", residentProtocolVersion, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{peerProtocol: tc.peer}
			err := c.checkResidentSupported(&model.Task{ID: "t", Kind: tc.kind})
			if tc.wantErr {
				testutil.Error(t, err)
				testutil.Contains(t, err.Error(), "restart the supervisor")
				return
			}
			testutil.NoError(t, err)
		})
	}
}

func TestStart_RefusesResidentOnOldSupervisor(t *testing.T) {
	c := &Client{peerProtocol: 12}
	res := &model.Task{ID: "t", Kind: model.KindResident}
	_, err := c.Start(res, configZero(), 24, 80, false)
	testutil.Error(t, err)
	testutil.Error(t, c.KickRerender(res, configZero(), 24, 80))
	testutil.Error(t, c.Recycle(res, configZero(), 24, 80))
}
