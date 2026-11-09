package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/claudeartifacts"
)

// Exit codes for `argus artifact publish`.
const (
	artifactExitOK          = 0
	artifactExitError       = 1
	artifactExitUsage       = 2
	artifactExitUnavailable = 3
)

const artifactUsage = "usage: argus artifact publish <file> --account <name> [--title <title>] [--update <link>]"

// artifactCLIEnv is how the artifact subcommand reaches the local daemon.
type artifactCLIEnv struct {
	// daemon returns the daemon's REST base URL and master token.
	daemon func() (baseURL, token string, err error)
	http   *http.Client
}

// realArtifactCLIEnv discovers the live daemon the same way coord-hook does:
// its REST port over the Unix socket, its master token from ~/.argus.
func realArtifactCLIEnv() artifactCLIEnv {
	return artifactCLIEnv{
		daemon: func() (string, string, error) {
			port, err := discoverAPIPort()
			if err != nil {
				return "", "", err
			}
			token, err := discoverAPIToken()
			if err != nil {
				return "", "", err
			}
			return coordHookBaseURL(port), token, nil
		},
		// Publishing can take a while: the upstream call plus, on an
		// unavailable account, a 15s probe of each other account.
		http: &http.Client{Timeout: 3 * time.Minute},
	}
}

// runArtifactCommand handles `argus artifact <subcommand> [args...]`.
func runArtifactCommand(args []string) {
	os.Exit(artifactCommand(args, realArtifactCLIEnv(), os.Stdout, os.Stderr))
}

// artifactCommand implements `argus artifact publish`: it publishes a local
// HTML page to claude.ai as --account through the daemon's
// POST /api/artifacts/publish. Exit 0 prints the link; exit 3 means the
// account is unavailable and lists the accounts that can publish; any other
// failure exits 1 (2 for usage errors).
func artifactCommand(args []string, env artifactCLIEnv, out, errOut io.Writer) int {
	if len(args) == 0 {
		fprintln(errOut, artifactUsage)
		return artifactExitUsage
	}
	if args[0] != "publish" {
		fprintf(errOut, "unknown artifact subcommand: %s\n%s\n", args[0], artifactUsage)
		return artifactExitUsage
	}
	opts, err := parseArtifactPublishArgs(args[1:])
	if err != nil {
		fprintf(errOut, "%v\n%s\n", err, artifactUsage)
		return artifactExitUsage
	}

	content, err := claudeartifacts.ReadHTMLFile(opts.file)
	if err != nil {
		fprintf(errOut, "argus artifact publish: %v\n", err)
		return artifactExitError
	}
	baseURL, token, err := env.daemon()
	if err != nil {
		fprintf(errOut, "argus artifact publish: cannot reach the Argus daemon: %v\n", err)
		return artifactExitError
	}
	resp, err := postArtifactPublish(env.http, baseURL, token, opts, content)
	if err != nil {
		fprintf(errOut, "argus artifact publish: %v\n", err)
		return artifactExitError
	}
	switch resp.Status {
	case "published":
		fprintln(out, resp.URL)
		return artifactExitOK
	case "unavailable":
		others := "no other configured account can publish right now"
		if len(resp.AvailableAccounts) > 0 {
			others = "accounts that can publish: " + strings.Join(resp.AvailableAccounts, ", ")
		}
		fprintf(errOut, "account %q is unavailable (its artifact tools are switched off, usually exhausted usage); %s\n", opts.account, others)
		return artifactExitUnavailable
	default:
		fprintf(errOut, "argus artifact publish: unexpected status %q from the daemon\n", resp.Status)
		return artifactExitError
	}
}

type artifactPublishOpts struct {
	file, account, title, update string
}

func parseArtifactPublishArgs(args []string) (artifactPublishOpts, error) {
	var o artifactPublishOpts
	var files []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--account", "--title", "--update":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s requires a value", a)
			}
			i++
			switch a {
			case "--account":
				o.account = args[i]
			case "--title":
				o.title = args[i]
			default:
				o.update = args[i]
			}
		default:
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown flag: %s", a)
			}
			files = append(files, a)
		}
	}
	switch {
	case len(files) == 0:
		return o, fmt.Errorf("a file to publish is required")
	case len(files) > 1:
		return o, fmt.Errorf("publish takes exactly one file, got %d", len(files))
	case o.account == "":
		return o, fmt.Errorf("--account is required")
	}
	o.file = files[0]
	return o, nil
}

// artifactPublishResponse mirrors the daemon's publish response body.
type artifactPublishResponse struct {
	Status            string   `json:"status"`
	URL               string   `json:"url"`
	AvailableAccounts []string `json:"available_accounts"`
	Error             string   `json:"error"`
}

func postArtifactPublish(client *http.Client, baseURL, token string, o artifactPublishOpts, content []byte) (artifactPublishResponse, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range [][2]string{{"account", o.account}, {"title", o.title}, {"update", o.update}} {
		if f[1] != "" {
			_ = mw.WriteField(f[0], f[1]) // writes to a bytes.Buffer cannot fail
		}
	}
	fw, _ := mw.CreateFormFile("file", filepath.Base(o.file))
	_, _ = fw.Write(content)
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/artifacts/publish", &body)
	if err != nil {
		return artifactPublishResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return artifactPublishResponse{}, fmt.Errorf("publish request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read to completion below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return artifactPublishResponse{}, fmt.Errorf("read response: %w", err)
	}
	var out artifactPublishResponse
	decodeErr := json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil && out.Error != "" {
			return out, fmt.Errorf("%s", out.Error)
		}
		return out, fmt.Errorf("daemon answered HTTP %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return out, fmt.Errorf("decode response: %w", decodeErr)
	}
	return out, nil
}
