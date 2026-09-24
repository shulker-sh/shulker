package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"shulker.sh/shulker/internal/account"
)

// The two account lists the official launcher writes, in the one directory it keeps them. The
// `_microsoft_store` suffix is per file rather than per install, so a directory may hold either or
// both and the reader takes the accounts from each; the packaged launcher's `Packages` tree holds
// no `launcher_*` file at all and is never looked at (docs/research/microsoft-sign-in.md).
const (
	MojangAccountsFile      = "launcher_accounts.json"
	MojangStoreAccountsFile = "launcher_accounts_microsoft_store.json"
)

var mojangAccountsFiles = []string{MojangAccountsFile, MojangStoreAccountsFile}

// mojangFile is one of those lists, keyed by the launcher's own local id. One struct decodes both
// files on both platforms, the Store file being a superset of the macOS shape. Neither
// `activeAccountLocalId` nor `mojangClientToken` is read, so two files disagreeing about which
// account is active costs nothing.
type mojangFile struct {
	Accounts map[string]mojangAccount `json:"accounts"`
}

// mojangAccount is one entry. The entitlements file beside it is never opened: the Java profile
// is itself the proof the account owns Java, Game Pass included, and an account without one is
// already not playable. The account's `username` is not read either — it is most likely the
// Microsoft email shulker took care never to handle.
type mojangAccount struct {
	AccessToken string         `json:"accessToken"`
	ExpiresAt   string         `json:"accessTokenExpiresAt"`
	Profile     *mojangProfile `json:"minecraftProfile"`
}

type mojangProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// mojangAccounts is the accounts the official launcher holds in dir. A directory or file that isn't
// there yields nothing and says nothing: opting in to a launcher that isn't installed is worth one
// line where the opting in happens, not on every run afterwards. Each file that doesn't read is an
// error to warn with and skip, the other file and every other provider still loading, since
// shulker neither wrote it nor can repair it and a corrupt one must not take `shulker accounts`
// down.
func mojangAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, []error) {
	var errs []error
	byID := map[string]mojangAccount{}
	for _, name := range mojangAccountsFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var file mojangFile
		if err := json.Unmarshal(data, &file); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		// By local id, so that two entries a file gives the same UUID and the same expiry
		// settle the same way on every run.
		for _, local := range slices.Sorted(maps.Keys(file.Accounts)) {
			file.Accounts[local].merge(byID)
		}
	}
	var out []account.Resolved
	for _, entry := range byID {
		out = append(out, entry.resolve(e, now))
	}
	account.Sort(out)
	return out, errs
}

// merge keeps one account per player UUID, since the same account can sit in both files. The entry
// whose token runs out later wins — the only thing the choice can affect, because username and
// UUID are equal by construction — and it is the one more likely to still work. An entry naming no
// Java profile is dropped: the username and the UUID both live there, so shulker could neither
// list nor launch it.
func (m mojangAccount) merge(byID map[string]mojangAccount) {
	if m.Profile == nil || m.Profile.ID == "" || m.Profile.Name == "" {
		return
	}
	id := account.NormalizeID(m.Profile.ID)
	if was, dup := byID[id]; dup && !m.expiry().After(was.expiry()) {
		return
	}
	byID[id] = m
}

// resolve is the borrowed account as every command sees it. An entry with a profile but no usable
// token is expired rather than skipped: the account is real, it proves the player owns Java, and a
// launch already says what an expired session can't do.
func (m mojangAccount) resolve(e *Entry, now time.Time) account.Resolved {
	r := account.Resolved{
		ID:      m.Profile.ID,
		Name:    m.Profile.Name,
		Source:  e.Name,
		Group:   account.GroupBorrowed,
		State:   account.TokenExpired,
		Account: account.Account{Type: account.Microsoft, Profile: &account.Profile{ID: m.Profile.ID, Name: m.Profile.Name}},
	}
	if m.AccessToken == "" {
		return r
	}
	r.Account.Minecraft = &account.Minecraft{Token: m.AccessToken}
	expires := m.expiry()
	if expires.IsZero() {
		return r
	}
	r.Account.Minecraft.ExpiresAt = expires.UTC().Format(time.RFC3339)
	// No margin, unlike an own account's IsFresh: nothing renews this one, so the token is spent
	// only once it actually runs out and is worth using until then.
	if expires.After(now) {
		r.State = account.Playable
		return r
	}
	r.Expired = expires
	return r
}

// expiry is when the session token runs out. The launcher writes .NET's seven fractional digits,
// which time.Parse takes on an RFC 3339 layout; a missing or unreadable one is no expiry at all,
// which reads as a token already spent.
func (m mojangAccount) expiry() time.Time {
	t, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return time.Time{}
	}
	return t
}
