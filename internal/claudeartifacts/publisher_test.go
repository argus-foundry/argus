package claudeartifacts

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

// fakeResolver maps secret sources to tokens and records what was asked.
type fakeResolver struct {
	mu      sync.Mutex
	tokens  map[string]string
	asked   []string
	gotSecs []config.SecretsConfig
}

func (r *fakeResolver) resolve(sc config.SecretsConfig, source string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, source)
	r.gotSecs = append(r.gotSecs, sc)
	v, ok := r.tokens[source]
	return v, ok
}

// byTokenHandler answers per bearer token: "tok-ok-*" publishes, "tok-gone-*"
// is the exhausted-account refusal, "tok-hang" blocks until the client gives up.
func byTokenHandler(t *testing.T) func(w http.ResponseWriter, c rpcCall) {
	ok := publishHandler(t)
	return func(w http.ResponseWriter, c rpcCall) {
		auth := strings.TrimPrefix(c.Header.Get("Authorization"), "Bearer ")
		switch {
		case strings.HasPrefix(auth, "tok-ok"):
			ok(w, c)
		case strings.HasPrefix(auth, "tok-gone"):
			unavailable(w)
		case auth == "tok-expired":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "expired")
		case auth == "tok-hang":
			<-c.Ctx.Done()
		default:
			t.Errorf("unexpected token %q", auth)
		}
	}
}

func testPublisher(t *testing.T, tokens map[string]string) (*Publisher, *fakeEndpoint, *fakeResolver) {
	t.Helper()
	f, srv := newFakeEndpoint(t, byTokenHandler(t))
	res := &fakeResolver{tokens: map[string]string{
		"op://claude/shell-env/PERSONAL": "tok-ok-personal",
		"op://claude/shell-env/WORK":     "tok-gone-work",
		"env://SPARE":                    "tok-ok-spare",
		"env://EXPIRED":                  "tok-expired",
		"env://HANG":                     "tok-hang",
	}}
	cfg := config.Config{
		ArtifactTokens: tokens,
		Secrets:        config.SecretsConfig{Op: config.OpConfig{BootstrapSource: "keychain://op", BootstrapTarget: "OP_SERVICE_ACCOUNT_TOKEN"}},
	}
	p := &Publisher{
		Client:  testClient(srv),
		Config:  func() config.Config { return cfg },
		Resolve: res.resolve,
	}
	return p, f, res
}

func TestPublisher_ResolvesTokenAtPublishTime(t *testing.T) {
	p, f, res := testPublisher(t, map[string]string{"personal": "op://claude/shell-env/PERSONAL"})
	result, err := p.Publish(context.Background(), "personal", PublishInput{Name: "p.html", Content: []byte(page), Title: "T"})
	testutil.NoError(t, err)
	testutil.Equal(t, result.URL, "https://claude.ai/artifact/NEWSLUG")
	testutil.DeepEqual(t, res.asked, []string{"op://claude/shell-env/PERSONAL"})
	testutil.Equal(t, res.gotSecs[0].Op.BootstrapSource, "keychain://op")
	testutil.Equal(t, f.call(0).Header.Get("Authorization"), "Bearer tok-ok-personal")
}

func TestPublisher_Errors(t *testing.T) {
	tokens := map[string]string{
		"personal": "op://claude/shell-env/PERSONAL",
		"work":     "op://claude/shell-env/WORK",
		"expired":  "env://EXPIRED",
		"broken":   "op://claude/shell-env/MISSING",
		"literal":  "tok-ok-literal-value",
	}
	for _, tc := range []struct {
		name, account string
		input         PublishInput
		wantIs        error
		wantText      []string
		wantRequests  int
	}{
		{
			name: "no token configured", account: "nobody",
			input: PublishInput{Name: "p.html", Content: []byte(page)}, wantIs: ErrNoToken,
			wantText: []string{"nobody", "[artifact_tokens]"},
		},
		{
			name: "literal token rejected", account: "literal",
			input: PublishInput{Name: "p.html", Content: []byte(page)}, wantIs: ErrNoToken,
			wantText: []string{"literal", "not a secret reference"},
		},
		{
			name: "secret does not resolve", account: "broken",
			input: PublishInput{Name: "p.html", Content: []byte(page)}, wantIs: ErrNoToken,
			wantText: []string{"broken", "could not resolve"},
		},
		{
			name: "invalid input before token", account: "nobody",
			input: PublishInput{Name: "p.txt", Content: []byte(page)}, wantIs: ErrInvalidInput,
		},
		{
			name: "expired token", account: "expired",
			input: PublishInput{Name: "p.html", Content: []byte(page)}, wantIs: ErrUnauthorized,
			wantText: []string{"claude setup-token", `"expired"`, "[artifact_tokens]"}, wantRequests: 1,
		},
		{
			name: "unavailable account", account: "work",
			input: PublishInput{Name: "p.html", Content: []byte(page)}, wantIs: ErrAccountUnavailable,
			wantText: []string{"work"}, wantRequests: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f, _ := testPublisher(t, tokens)
			_, err := p.Publish(context.Background(), tc.account, tc.input)
			testutil.ErrorIs(t, err, tc.wantIs)
			for _, w := range tc.wantText {
				testutil.Contains(t, err.Error(), w)
			}
			testutil.False(t, strings.Contains(err.Error(), "tok-"))
			testutil.Equal(t, f.count(), tc.wantRequests)
		})
	}
}

func TestPublisher_AvailableAccounts(t *testing.T) {
	p, _, _ := testPublisher(t, map[string]string{
		"default":  "op://claude/shell-env/WORK", // the excluded (requesting) account
		"personal": "op://claude/shell-env/PERSONAL",
		"spare":    "env://SPARE",
		"work":     "op://claude/shell-env/WORK",
		"broken":   "op://claude/shell-env/MISSING",
		"literal":  "not-a-reference",
		"expired":  "env://EXPIRED",
	})
	got := p.AvailableAccounts(context.Background(), "default")
	testutil.DeepEqual(t, got, []string{"personal", "spare"})
}

func TestPublisher_AvailableAccountsTimeout(t *testing.T) {
	p, _, _ := testPublisher(t, map[string]string{
		"personal": "op://claude/shell-env/PERSONAL",
		"slow":     "env://HANG",
	})
	p.ProbeTimeout = 100 * time.Millisecond
	start := time.Now()
	got := p.AvailableAccounts(context.Background(), "")
	testutil.DeepEqual(t, got, []string{"personal"})
	testutil.True(t, time.Since(start) < 2*time.Second)
}

func TestPublisher_AvailableAccountsEmpty(t *testing.T) {
	p, f, _ := testPublisher(t, nil)
	testutil.Equal(t, len(p.AvailableAccounts(context.Background(), "default")), 0)
	testutil.Equal(t, f.count(), 0)
}

func TestPublisher_DefaultProbeTimeout(t *testing.T) {
	testutil.Equal(t, (&Publisher{}).probeTimeout(), 15*time.Second)
	testutil.Equal(t, (&Publisher{ProbeTimeout: time.Second}).probeTimeout(), time.Second)
}

func TestPublisher_BadUpdateLinkMakesNoRequest(t *testing.T) {
	p, f, res := testPublisher(t, map[string]string{"personal": "op://claude/shell-env/PERSONAL"})
	_, err := p.Publish(context.Background(), "personal", PublishInput{Name: "p.html", Content: []byte(page), UpdateURL: "https://example.com/a/b"})
	testutil.ErrorIs(t, err, ErrInvalidInput)
	testutil.Equal(t, f.count(), 0)
	testutil.Equal(t, len(res.asked), 0)
}

func TestPublisher_OtherUpstreamError(t *testing.T) {
	_, srv := newFakeEndpoint(t, func(w http.ResponseWriter, _ rpcCall) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	})
	p := &Publisher{
		Client:  testClient(srv),
		Config:  func() config.Config { return config.Config{ArtifactTokens: map[string]string{"a": "env://A"}} },
		Resolve: func(config.SecretsConfig, string) (string, bool) { return "tok-a", true },
	}
	_, err := p.Publish(context.Background(), "a", PublishInput{Name: "p.html", Content: []byte(page)})
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), `account "a"`)
	testutil.Contains(t, err.Error(), "HTTP 500")
}
