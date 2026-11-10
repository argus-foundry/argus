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

func TestResOldSup(t *testing.T) {
	c, rec := newWireRecorderClient(t)
	setPeer := func(v int) {
		c.mu.Lock()
		c.peerProtocol = v
		c.mu.Unlock()
	}
	res := &model.Task{ID: "t", Kind: model.KindResident}

	setPeer(12)
	_, err := c.Start(res, configZero(), 24, 80, false)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "restart the supervisor")
	for name, call := range map[string]func() error{
		"kick":    func() error { return c.KickRerender(res, configZero(), 24, 80) },
		"revive":  func() error { return c.Revive(res, configZero(), 24, 80) },
		"width":   func() error { return c.KickWidthRerender(res, configZero(), 24, 80) },
		"recycle": func() error { return c.Recycle(res, configZero(), 24, 80) },
	} {
		err := call()
		testutil.Error(t, err)
		if err != nil {
			testutil.Contains(t, err.Error(), "restart the supervisor")
		} else {
			t.Errorf("%s did not refuse", name)
		}
	}
	rec.mu.Lock()
	testutil.Equal(t, rec.start.TaskID, "")
	testutil.Equal(t, rec.kick.TaskID, "")
	testutil.Equal(t, rec.ordinaryKicks, 0)
	testutil.Equal(t, rec.boundedKicks, 0)
	testutil.Equal(t, rec.recycle.TaskID, "")
	rec.mu.Unlock()

	// Control: a supervisor at the resident protocol and the per-launch
	// identity spawn surface (44) accepts the same task.
	setPeer(residentProtocolVersion)
	c.mu.Lock()
	c.peerSpawnSurface = 44
	c.mu.Unlock()
	_, err = c.Start(res, configZero(), 24, 80, false)
	testutil.NoError(t, err)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	testutil.Equal(t, rec.start.TaskID, "t")
	testutil.Equal(t, rec.start.Kind, model.KindResident)
}
