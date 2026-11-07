package gitutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drn/argus/internal/model"
)

var prURLSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ErrInvalidPRURL is returned for anything that is not a github.com pull
// request URL.
var ErrInvalidPRURL = errors.New("invalid pull request url")

// ParsePRURL validates raw as https://github.com/<owner>/<repo>/pull/<n> and
// returns its canonical form. Query and fragment are dropped; any other host,
// scheme, userinfo, port or extra path segment is rejected.
func ParsePRURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.EqualFold(u.Hostname(), "github.com") {
		return "", ErrInvalidPRURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || !prURLSegment.MatchString(parts[0]) || !prURLSegment.MatchString(parts[1]) {
		return "", ErrInvalidPRURL
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "", ErrInvalidPRURL
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", parts[0], parts[1], n), nil
}

// FetchPRByURL resolves one canonical PR url through gh. A non-nil error means
// the result is transient or unverifiable and the caller must keep its cache.
func FetchPRByURL(ctx context.Context, prURL string) (PRResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, code, runErr := prRunner(fetchCtx, "", "pr", "view", prURL, "--json", "state,isDraft,reviewDecision,url")
	if runErr != nil && code == 0 {
		return PRResult{}, fmt.Errorf("gh: %w", runErr)
	}
	state, url, err := mapPRState(raw, code)
	if err != nil {
		return PRResult{}, err
	}
	if state == model.PRNone {
		return PRResult{}, fmt.Errorf("gh found no pull request at %s", prURL)
	}
	var p prJSON
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return PRResult{}, fmt.Errorf("parse gh json: %w", err)
	}
	return PRResult{State: state, URL: url, Merged: p.State == "MERGED"}, nil
}
