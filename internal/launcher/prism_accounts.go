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

// PrismAccountsFile is what Prism calls its account list inside its own data directory
// (Application.cpp: setListFilePath("accounts.json", true)).
const PrismAccountsFile = "accounts.json"

// prismVersion is the only list version Prism reads (AccountList.cpp, AccountListVersion::MojangMSA);
// it renames a file carrying any other and starts a fresh list, so shulker can't read one either.
const prismVersion = 3

// prismSince is how long a token Prism wrote no expiry for lasts, which is the fallback
// MinecraftAccount::shouldRefresh applies to an invalid notAfter.
const prismSince = 24 * time.Hour

const (
	prismMSA     = "MSA"
	prismOffline = "Offline"
)

// prismFile is Prism's accounts.json, as AccountList::saveList writes it.
type prismFile struct {
	FormatVersion int            `json:"formatVersion"`
	Accounts      []prismAccount `json:"accounts"`
}

// prismAccount is one entry of that list (AccountData::saveState). Only the profile and the
// yggdrasil session token are read: shulker never renews a launcher account, so the Microsoft,
// Xbox and XSTS tokens beside them — msa, utoken and xrp-mc — are of no use to it.
type prismAccount struct {
	Type    string        `json:"type"`
	Profile *prismProfile `json:"profile"`
	Ygg     *prismToken   `json:"ygg"`
}

type prismProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type prismToken struct {
	Token    string `json:"token"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// prismAccounts is the accounts Prism or MultiMC holds in dir, since both write this list. A
// directory or file that isn't there yields nothing and says nothing: opting in to a launcher that
// isn't installed is worth one line where the opting in happens, not on every run afterwards. A
// file that doesn't read is an error to warn with and skip, since shulker neither wrote it nor can
// repair it and a corrupt one must not take `shulker accounts` down.
func prismAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, []error) {
	found, err := readPrismAccounts(e, dir, now)
	if err != nil {
		return found, []error{err}
	}
	return found, nil
}

func readPrismAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, error) {
	path := filepath.Join(dir, PrismAccountsFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file prismFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if file.FormatVersion != prismVersion {
		return nil, fmt.Errorf("%s: format version %d, not the %d shulker reads", path, file.FormatVersion, prismVersion)
	}
	var out []account.Resolved
	for _, entry := range file.Accounts {
		if r, ok := entry.resolve(e, now); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// resolve is one launcher account, or nothing when the entry names none shulker can use. An entry
// with no profile is skipped silently, because the username and the UUID both live there and the
// file's own username field is most likely the Microsoft email shulker took care never to handle.
// A type Prism itself refuses to load goes the same way.
func (p prismAccount) resolve(e *Entry, now time.Time) (account.Resolved, bool) {
	if p.Profile == nil || p.Profile.ID == "" || p.Profile.Name == "" {
		return account.Resolved{}, false
	}
	r := account.Resolved{
		ID:      p.Profile.ID,
		Name:    p.Profile.Name,
		Source:  e.Name,
		Group:   account.GroupLauncher,
		State:   account.OfflineOnly,
		Account: account.Account{Type: account.Offline, Profile: &account.Profile{ID: p.Profile.ID, Name: p.Profile.Name}},
	}
	switch p.Type {
	case prismOffline:
		return r, true
	case prismMSA:
	default:
		return account.Resolved{}, false
	}
	r.Account.Type = account.Microsoft
	r.State = account.TokenExpired
	if p.Ygg == nil || p.Ygg.Token == "" {
		return r, true
	}
	r.Account.Minecraft = &account.Minecraft{Token: p.Ygg.Token}
	expires := p.Ygg.expiry()
	if expires.IsZero() {
		return r, true
	}
	r.Account.Minecraft.ExpiresAt = expires.UTC().Format(time.RFC3339)
	// No margin, unlike an own account's IsFresh: nothing renews this one, so the token is spent
	// only once it actually runs out and is worth using until then.
	if expires.After(now) {
		r.State = account.Playable
		return r, true
	}
	r.Expired = expires
	return r, true
}

func (t prismToken) expiry() time.Time {
	switch {
	case t.Expires != 0:
		return time.Unix(t.Expires, 0)
	case t.IssuedAt != 0:
		return time.Unix(t.IssuedAt, 0).Add(prismSince)
	}
	return time.Time{}
}
