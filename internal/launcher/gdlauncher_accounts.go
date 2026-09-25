package launcher

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shulker.sh/shulker/internal/account"

	_ "modernc.org/sqlite"
)

// GDLauncherAccountsDB is GDLauncher's database in its runtime directory, whose Account table holds
// its accounts.
const GDLauncherAccountsDB = "gdl_conf.db"

// gdlauncherAccounts is the accounts GDLauncher holds in dir. A missing database says nothing and
// one that doesn't read warns, as for Prism.
func gdlauncherAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, []error) {
	found, err := readGDLauncherAccounts(e, dir, now)
	if err != nil {
		return found, []error{err}
	}
	return found, nil
}

// readGDLauncherAccounts goes through SQLite rather than the file, because GDLauncher keeps the
// database in WAL mode and a sign-in it made while running may not have reached the file yet. The
// connection is read-only, so nothing of GDLauncher's is written.
func readGDLauncherAccounts(e *Entry, dir string, now time.Time) ([]account.Resolved, error) {
	path := filepath.Join(dir, GDLauncherAccountsDB)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT "uuid", "username", "accessToken", "tokenExpires" FROM "Account" ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer rows.Close()
	var out []account.Resolved
	for rows.Next() {
		var a gdlauncherAccount
		if err := rows.Scan(&a.UUID, &a.Name, &a.AccessToken, &a.Expires); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if r, ok := a.resolve(e, now); ok {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// readOnlyURI names path as a SQLite URI opened read-only. The path is escaped, since the default
// runtime directory sits under "Application Support" on macOS, and a Windows drive letter needs
// the leading slash a URI path has.
func readOnlyURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}).String()
}

// gdlauncherAccount is one Account row. uuid and username are the Java profile; the Microsoft
// tokens beside them only renew an account, which shulker never does.
type gdlauncherAccount struct {
	UUID        string
	Name        string
	AccessToken sql.NullString
	Expires     any
}

// resolve takes a row with no access token as the offline account GDLauncher stores that way.
func (a gdlauncherAccount) resolve(e *Entry, now time.Time) (account.Resolved, bool) {
	if a.UUID == "" || a.Name == "" {
		return account.Resolved{}, false
	}
	r := account.Resolved{
		ID:      a.UUID,
		Name:    a.Name,
		Source:  e.Name,
		Group:   account.GroupLauncher,
		State:   account.OfflineOnly,
		Account: account.Account{Type: account.Offline, Profile: &account.Profile{ID: a.UUID, Name: a.Name}},
	}
	if !a.AccessToken.Valid {
		return r, true
	}
	r.Account.Type = account.Microsoft
	r.State = account.TokenExpired
	if a.AccessToken.String == "" {
		return r, true
	}
	r.Account.Minecraft = &account.Minecraft{Token: a.AccessToken.String}
	expires := gdlauncherTime(a.Expires)
	if expires.IsZero() {
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

// gdlauncherTime reads a DATETIME column the way GDLauncher's DbDateTime does: Unix milliseconds as
// an integer, or the text a CURRENT_TIMESTAMP default leaves.
func gdlauncherTime(v any) time.Time {
	switch v := v.(type) {
	case int64:
		return time.UnixMilli(v)
	case time.Time:
		return v
	case string:
		for _, layout := range []string{time.DateTime, time.RFC3339Nano} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
