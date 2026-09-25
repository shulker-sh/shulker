package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"shulker.sh/shulker/internal/account"
)

// ATLauncherAccountsFile is ATLauncher's account list under its base directory (FileSystem.ACCOUNTS).
var ATLauncherAccountsFile = filepath.Join("configs", "accounts.json")

// atlauncherAccount is one MicrosoftAccount as AccountManager.saveAccounts writes it. Only the Java
// profile and the Minecraft session token are read: username is the Microsoft login name, and the
// oauthToken and xstsAuth beside them only renew an account, which shulker never does.
type atlauncherAccount struct {
	UUID        string           `json:"uuid"`
	Name        string           `json:"minecraftUsername"`
	AccessToken string           `json:"accessToken"`
	ExpiresAt   string           `json:"accessTokenExpiresAt"`
	OAuth       *atlauncherOAuth `json:"oauthToken"`
}

type atlauncherOAuth struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// atlauncherAccounts is the accounts ATLauncher holds in dir, which are all Microsoft ones: it has
// no offline accounts. A missing file says nothing and a corrupt one warns, as for Prism.
func atlauncherAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, []error) {
	found, err := readATLauncherAccounts(e, dir, now)
	if err != nil {
		return found, []error{err}
	}
	return found, nil
}

func readATLauncherAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, error) {
	path := filepath.Join(dir, ATLauncherAccountsFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file []atlauncherAccount
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var out []account.Resolved
	for _, entry := range file {
		if r, ok := entry.resolve(e, now); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// resolve skips what ATLauncher's own loadAccounts drops (isAcceptedMicrosoftAccount) and an entry
// with no Java profile. An expiry that isn't the ISO form ATLauncher writes today, such as the en-US
// text of an older file, leaves the token undated, so it is used and warned about.
func (a atlauncherAccount) resolve(e *Entry, now time.Time) (account.Resolved, bool) {
	if a.UUID == "" || a.Name == "" || a.AccessToken == "" ||
		a.OAuth == nil || a.OAuth.AccessToken == "" || a.OAuth.RefreshToken == "" {
		return account.Resolved{}, false
	}
	r := account.Resolved{
		ID:     a.UUID,
		Name:   a.Name,
		Source: e.Name,
		Group:  account.GroupLauncher,
		State:  account.TokenExpired,
		Account: account.Account{
			Type:      account.Microsoft,
			Profile:   &account.Profile{ID: a.UUID, Name: a.Name},
			Minecraft: &account.Minecraft{Token: a.AccessToken},
		},
	}
	expires, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return r, true
	}
	r.Account.Minecraft.ExpiresAt = expires.UTC().Format(time.RFC3339)
	if expires.After(now) {
		r.State = account.Playable
		return r, true
	}
	r.Expired = expires
	return r, true
}
