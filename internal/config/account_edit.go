package config

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"strings"
)

// Kept out of accounts.go on purpose: that file is in the supervisor
// spawn-surface manifest and these helpers are Settings/REST-edit only.

var accountNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateNewAccountName checks a name for a brand-new account against the
// existing ones (case-insensitively, since "Work" and "work" would collide on
// a case-insensitive filesystem).
func ValidateNewAccountName(name string, existing map[string]Account) error {
	switch {
	case !accountNameRE.MatchString(name):
		return fmt.Errorf("invalid name %q: use letters, digits, - and _ only", name)
	case strings.EqualFold(name, DefaultAccountName):
		return fmt.Errorf("invalid name %q: reserved for the tool defaults", name)
	}
	for other := range existing {
		if strings.EqualFold(other, name) {
			return fmt.Errorf("account %q already exists", other)
		}
	}
	return nil
}

// ValidateAccountEdit checks account a as it would be stored under name among
// the existing accounts: the config.toml rules (ValidateAccount) plus no
// directory shared with another account.
func ValidateAccountEdit(existing map[string]Account, name string, a Account) error {
	accounts := maps.Clone(existing)
	if accounts == nil {
		accounts = map[string]Account{}
	}
	accounts[name] = a
	if err := (Config{Accounts: accounts}).ValidateAccount(name); err != nil {
		return err
	}
	for other, o := range existing {
		if other == name {
			continue
		}
		if sameAccountDir(a.ClaudeConfigDir, o.ClaudeConfigDir) || sameAccountDir(a.CodexHome, o.CodexHome) {
			return fmt.Errorf("directory already used by account %q (accounts must not share a login)", other)
		}
	}
	return nil
}

func sameAccountDir(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && b != "" && filepath.Clean(ExpandHome(a)) == filepath.Clean(ExpandHome(b))
}
