package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The agent signing helper (add-agent-signing-key D4). Under the agent-key
// policy, agent sessions set gpg.ssh.program to the argus executable. Git
// execs that program directly (no shell) with ssh-keygen's argv, so main.go
// dispatches `argus -Y ...` here before any other startup work.

// gitSignRetryStep is the pause between attempts while the daemon is
// unreachable (a bounce: socket gone, port lookup failing, connection
// refused).
const gitSignRetryStep = 500 * time.Millisecond

// gitSignBlocker is appended to every failure so the agent escalates instead
// of committing unsigned.
const gitSignBlocker = "report this to your coordinator as a blocker; do not retry unsigned"

// gitSignEnv is everything the helper reaches outside its own logic.
type gitSignEnv struct {
	// daemon returns the daemon's REST base URL and bearer token.
	daemon func() (baseURL, token string, err error)
	http   *http.Client
	// sshKeygen runs the real ssh-keygen with args and returns its exit code.
	sshKeygen func(args []string, stdout, stderr io.Writer) int
	// retryFor bounds the SUM of sleeps spent retrying connection and
	// discovery failures. Never measured with time.Now.
	retryFor time.Duration
	sleep    func(time.Duration)
	taskID   string
}

// realGitSignEnv discovers the daemon like `argus artifact`: REST port over
// the Unix socket, master token from ~/.argus.
//
// Test override: when BOTH ARGUS_GIT_SIGN_URL and ARGUS_GIT_SIGN_TOKEN are
// set, they replace discovery (the end-to-end test points a real git commit
// at an in-test signing server this way). The override cannot leak the master
// token, since the token then comes from the same environment as the URL and
// the ~/.argus token file is never read; all it can change is where the
// caller's own commit payload is sent for signing, which the caller already
// holds.
func realGitSignEnv() gitSignEnv {
	return gitSignEnv{
		daemon: func() (string, string, error) {
			if u, tok := os.Getenv("ARGUS_GIT_SIGN_URL"), os.Getenv("ARGUS_GIT_SIGN_TOKEN"); u != "" && tok != "" {
				return u, tok, nil
			}
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
		// Must outlast the daemon's worst-case first key resolve: two `op
		// read`s (15s each) plus the Keychain bootstrap (5s).
		http:      &http.Client{Timeout: 60 * time.Second},
		sshKeygen: runSSHKeygen,
		retryFor:  30 * time.Second,
		sleep:     time.Sleep,
		taskID:    os.Getenv("ARGUS_TASK_ID"),
	}
}

// runSSHKeygen execs the system ssh-keygen with args, passing stdio through,
// and returns its exit code unchanged.
func runSSHKeygen(args []string, stdout, stderr io.Writer) int {
	path, err := exec.LookPath("ssh-keygen")
	if err != nil {
		fprintf(stderr, "argus: ssh-keygen not found on PATH: %v\n", err)
		return 255
	}
	cmd := exec.Command(path, args...) //nolint:gosec // passthrough of git's own ssh-keygen argv
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fprintf(stderr, "argus: run ssh-keygen: %v\n", err)
		return 255
	}
	return 0
}

// gitSignCommand implements `argus -Y <op> ...`. args starts at "-Y". Only
// `-Y sign` is handled here; every other operation (verify, find-principals,
// check-novalidate, ...) runs the real ssh-keygen with the same arguments.
func gitSignCommand(args []string, env gitSignEnv, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[1] != "sign" {
		return env.sshKeygen(args, stdout, stderr)
	}
	if err := gitSign(args[2:], env); err != nil {
		fprintf(stderr, "argus: agent commit signing failed: %v — %s\n", err, gitSignBlocker)
		return 1
	}
	return 0
}

// gitSign parses `-Y sign` options, signs the data file through the daemon,
// and writes <file>.sig. No .sig is written on any failure.
func gitSign(args []string, env gitSignEnv) error {
	var namespace string
	var files []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-n", "-f", "-O":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			if a == "-n" {
				namespace = args[i+1]
			}
			// -f names the public key file (the daemon holds the key) and
			// -O passes ssh-keygen options: both are accepted and ignored.
			i++
		case "-U":
			// "key is in an agent": irrelevant, the daemon holds the key.
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return fmt.Errorf("unsupported ssh-keygen sign option %s", a)
			}
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return errors.New("no data file to sign")
	}
	dataPath := files[len(files)-1]
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return fmt.Errorf("read data file: %w", err)
	}
	sig, err := requestGitSignature(env, namespace, data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dataPath+".sig", sig, 0o644); err != nil { //nolint:gosec // a public signature, same mode as ssh-keygen
		return fmt.Errorf("write signature: %w", err)
	}
	return nil
}

// requestGitSignature POSTs to /api/git/sign, retrying discovery and
// connection failures until the slept total reaches env.retryFor. Any HTTP
// response is final.
func requestGitSignature(env gitSignEnv, namespace string, data []byte) ([]byte, error) {
	body, err := json.Marshal(map[string]string{
		"namespace": namespace,
		"data":      base64.StdEncoding.EncodeToString(data),
		"task_id":   env.taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	var slept time.Duration
	for {
		sig, retryable, err := postGitSign(env, body)
		if err == nil {
			return sig, nil
		}
		if !retryable {
			return nil, err
		}
		if slept >= env.retryFor {
			return nil, fmt.Errorf("Argus daemon unreachable for %s: %w", env.retryFor, err)
		}
		d := min(gitSignRetryStep, env.retryFor-slept)
		env.sleep(d)
		slept += d
	}
}

// postGitSign makes one attempt. retryable is true only when the daemon was
// never reached (discovery or connection failure).
func postGitSign(env gitSignEnv, body []byte) (sig []byte, retryable bool, err error) {
	baseURL, token, err := env.daemon()
	if err != nil {
		return nil, true, fmt.Errorf("cannot reach the Argus daemon: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/git/sign", bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.http.Do(req)
	if err != nil {
		// A timeout means the daemon took the request and never answered:
		// retrying could stack up minutes of waiting, so it is final.
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, false, fmt.Errorf("sign request timed out: %w", err)
		}
		return nil, true, fmt.Errorf("sign request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read to completion below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("read response: %w", err)
	}
	var out struct {
		Signature string `json:"signature"`
		Error     string `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil && out.Error != "" {
			return nil, false, fmt.Errorf("daemon refused (HTTP %d): %s", resp.StatusCode, out.Error)
		}
		return nil, false, fmt.Errorf("daemon answered HTTP %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return nil, false, fmt.Errorf("decode response: %w", decodeErr)
	}
	if out.Signature == "" {
		return nil, false, errors.New("daemon returned an empty signature")
	}
	return []byte(out.Signature), false, nil
}
