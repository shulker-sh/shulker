package launcher

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shulker.sh/shulker/internal/account"
)

// gdlauncherAccountTable is the Account table as GDLauncher's migrations leave it: the init
// migration's columns plus add_gdl_token's gdlToken.
const gdlauncherAccountTable = `CREATE TABLE "Account" (
    "uuid" TEXT NOT NULL PRIMARY KEY,
    "username" TEXT NOT NULL,
    "accessToken" TEXT,
    "tokenExpires" DATETIME,
    "msRefreshToken" TEXT,
    "idToken" TEXT,
    "lastUsed" DATETIME NOT NULL,
    "skinId" TEXT,
    "gdlToken" TEXT
)`

// openGDLauncherDB makes a gdl_conf.db in WAL mode, under a folder named like macOS's default, and
// keeps it open the way a running GDLauncher holds it, so rows written through it may sit in the
// WAL rather than the database.
func openGDLauncherDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, GDLauncherAccountsDB))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{"PRAGMA journal_mode = WAL", "PRAGMA wal_autocheckpoint = 0", gdlauncherAccountTable} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return dir, db
}

func insertGDLauncherAccount(t *testing.T, db *sql.DB, uuid, username string, token, expires any) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO "Account" ("uuid", "username", "accessToken", "tokenExpires", "msRefreshToken", "idToken", "lastUsed")
		VALUES (?, ?, ?, ?, 'refresh', 'id', ?)`, uuid, username, token, expires, prismNow.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadGDLauncherAccounts(t *testing.T) {
	dir, db := openGDLauncherDB(t)
	insertGDLauncherAccount(t, db, "069a79f44e9a4726a5befca90e38aaf5", "Notch", "session", time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).UnixMilli())
	insertGDLauncherAccount(t, db, "853c80ef3c3749fdaa49938b674adae6", "Jeb_", "stale", time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC).UnixMilli())
	insertGDLauncherAccount(t, db, "5627dd98e6be3c21b8a8e92344183641", "Steve", nil, nil)
	if _, err := os.Stat(filepath.Join(dir, GDLauncherAccountsDB+"-wal")); err != nil {
		t.Fatalf("the rows should still be in the WAL: %v", err)
	}

	got, errs := gdlauncherEntry.Accounts(gdlauncherEntry, dir, prismNow)
	if errs != nil {
		t.Fatal(errs)
	}
	byName := map[string]account.Resolved{}
	for _, r := range got {
		byName[r.Name] = r
	}
	if len(byName) != 3 {
		t.Fatalf("accounts = %+v", got)
	}
	notch := byName["Notch"]
	if notch.ID != "069a79f44e9a4726a5befca90e38aaf5" || notch.Source != "gdlauncher" || notch.Group != account.GroupLauncher {
		t.Errorf("row = %+v", notch)
	}
	if notch.State != account.Playable || notch.Account.Type != account.Microsoft || notch.Account.Minecraft == nil || notch.Account.Minecraft.Token != "session" {
		t.Errorf("a token that still holds is playable: %+v", notch)
	}
	if notch.Account.RefreshToken != "" {
		t.Error("a launcher account is never renewed, so no refresh token is kept")
	}
	jeb := byName["Jeb_"]
	if jeb.State != account.TokenExpired || jeb.Account.Minecraft == nil || jeb.Account.Minecraft.Token != "stale" {
		t.Errorf("an expired launcher account keeps its token: %+v", jeb)
	}
	if text := jeb.State.Text(jeb.Expired, prismNow); text != "token expired 2 days ago" {
		t.Errorf("state text = %q", text)
	}
	if steve := byName["Steve"]; steve.State != account.OfflineOnly || steve.Account.Type != account.Offline {
		t.Errorf("a row with no access token is an offline account: %+v", steve)
	}
}

func TestReadGDLauncherTokenExpiry(t *testing.T) {
	for _, c := range []struct {
		name    string
		expires any
		want    account.State
		expired time.Time
	}{
		{"holds", time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).UnixMilli(), account.Playable, time.Time{}},
		{"ran out", time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC).UnixMilli(), account.TokenExpired, time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)},
		{"CURRENT_TIMESTAMP text", "2026-09-21 00:00:00", account.Playable, time.Time{}},
		{"undated", nil, account.TokenExpired, time.Time{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, db := openGDLauncherDB(t)
			insertGDLauncherAccount(t, db, "069a79f44e9a4726a5befca90e38aaf5", "Notch", "s", c.expires)
			got, errs := gdlauncherEntry.Accounts(gdlauncherEntry, dir, prismNow)
			if errs != nil || len(got) != 1 {
				t.Fatalf("read = %+v %v", got, errs)
			}
			if got[0].State != c.want || !got[0].Expired.Equal(c.expired) {
				t.Errorf("state = %q expired %v, want %q %v", got[0].State, got[0].Expired, c.want, c.expired)
			}
		})
	}
}

func TestReadGDLauncherSilentAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if got, errs := gdlauncherEntry.Accounts(gdlauncherEntry, dir, prismNow); got != nil || errs != nil {
		t.Errorf("no database says nothing: %+v %v", got, errs)
	}
	if _, err := os.Stat(filepath.Join(dir, GDLauncherAccountsDB)); err == nil {
		t.Error("reading must not create the database")
	}
	if err := os.WriteFile(filepath.Join(dir, GDLauncherAccountsDB), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs := gdlauncherEntry.Accounts(gdlauncherEntry, dir, prismNow); len(errs) != 1 {
		t.Errorf("a database that doesn't read warns: %v", errs)
	}
}

func TestReadGDLauncherWhileClosed(t *testing.T) {
	dir, db := openGDLauncherDB(t)
	insertGDLauncherAccount(t, db, "069a79f44e9a4726a5befca90e38aaf5", "Notch", "session", time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).UnixMilli())
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, GDLauncherAccountsDB+"-wal")); err == nil {
		t.Fatal("closing the last connection should checkpoint and remove the WAL")
	}
	got, errs := gdlauncherEntry.Accounts(gdlauncherEntry, dir, prismNow)
	if errs != nil || len(got) != 1 || got[0].Name != "Notch" {
		t.Fatalf("read = %+v %v", got, errs)
	}
}
