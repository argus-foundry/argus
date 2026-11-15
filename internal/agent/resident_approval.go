package agent

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
)

const residentApprovalFile = "approval-hook.json"

// ResidentApprovalLaunch is private launch material. Only Token is secret;
// Generation is its public fingerprint, safe in the hook command's argv.
type ResidentApprovalLaunch struct {
	Token, Generation, TaskID, Session, Cwd, Account string
}

func ResidentApprovalLaunchPath() string {
	return filepath.Join(db.DataDir(), "resident-launch", residentApprovalFile)
}

func ReadResidentApprovalLaunch() (ResidentApprovalLaunch, error) {
	var launch ResidentApprovalLaunch
	path := ResidentApprovalLaunchPath()
	st, err := os.Lstat(path)
	if err != nil {
		return launch, err
	}
	if !st.Mode().IsRegular() || st.Size() > 8192 {
		return launch, errors.New("invalid resident approval launch file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return launch, err
	}
	if err := json.Unmarshal(raw, &launch); err != nil {
		return ResidentApprovalLaunch{}, err
	}
	if launch.Token == "" || launch.Generation != HashResidentMCPToken(launch.Token) {
		return ResidentApprovalLaunch{}, errors.New("invalid resident approval credential")
	}
	return launch, nil
}

func (l ResidentApprovalLaunch) Matches(token, generation string) bool {
	return l.Token != "" && l.Generation == generation && subtle.ConstantTimeCompare([]byte(l.Token), []byte(token)) == 1
}

// writeResidentApprovalHook rotates authorization even for a same-session
// resume. A stale process cannot read the new credential: its hook command
// carries the expected public generation, checked before sending any RPC.
func writeResidentApprovalHook(task *model.Task) (string, error) {
	dir, err := residentLaunchDir()
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	launch := ResidentApprovalLaunch{Token: token, Generation: HashResidentMCPToken(token), TaskID: task.ID, Session: task.SessionID, Cwd: task.Worktree, Account: task.Account}
	raw, err := json.Marshal(launch)
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(ResidentApprovalLaunchPath(), raw); err != nil {
		return "", err
	}
	exe, err := residentExecutable()
	if err != nil || exe == "" {
		return "", errors.New("resident approval hook executable unavailable")
	}
	if !shellSafeWord.MatchString(exe) {
		exe = shellQuote(exe)
	}
	return exe + " resident-permission-hook " + launch.Generation, nil
}
