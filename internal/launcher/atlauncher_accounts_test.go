package launcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"shulker.sh/shulker/internal/account"
)

func writeATLauncherAccounts(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ATLauncherAccountsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func atlauncherAccountJSON(uuid, name, token, expires string) string {
	return `{
	  "accessToken": "` + token + `",
	  "oauthToken": {"token_type": "bearer", "expires_in": 86400, "scope": "XboxLive.signin offline_access",
	    "access_token": "ms", "refresh_token": "refresh", "user_id": "u", "expires_at": "2026-09-21T12:00:00Z"},
	  "xstsAuth": {"IssueInstant": "2026-09-20T11:00:00.0000000Z", "NotAfter": "2026-09-21T03:00:00.0000000Z",
	    "Token": "xsts", "DisplayClaims": {"xui": [{"uhs": "h"}]}},
	  "accessTokenExpiresAt": "` + expires + `",
	  "mustLogin": false,
	  "username": "player@example.com",
	  "minecraftUsername": "` + name + `",
	  "uuid": "` + uuid + `",
	  "skinId": "s"
	}`
}

func TestReadATLauncherAccounts(t *testing.T) {
	dir := writeATLauncherAccounts(t, `[`+
		atlauncherAccountJSON("069a79f44e9a4726a5befca90e38aaf5", "Notch", "session", "2026-09-21T00:00:00Z")+`,`+
		atlauncherAccountJSON("853c80ef3c3749fdaa49938b674adae6", "Jeb_", "stale", "2026-09-18T12:00:00Z")+`,`+
		atlauncherAccountJSON("", "", "orphan", "2026-09-21T00:00:00Z")+`,`+
		`{"accessToken": "x", "minecraftUsername": "Dinnerbone", "uuid": "61699b2ed3274a019f1e0ea8c3f06bc6"}`+
		`]`)

	got, errs := atlauncherEntry.Accounts(atlauncherEntry, dir, prismNow)
	if errs != nil {
		t.Fatal(errs)
	}
	if len(got) != 2 {
		t.Fatalf("only entries with a profile ATLauncher would load are listed: %+v", got)
	}
	notch, jeb := got[0], got[1]
	if notch.ID != "069a79f44e9a4726a5befca90e38aaf5" || notch.Name != "Notch" || notch.Source != "atlauncher" || notch.Group != account.GroupLauncher {
		t.Errorf("row = %+v", notch)
	}
	if notch.State != account.Playable || notch.Account.Type != account.Microsoft || notch.Account.Minecraft == nil || notch.Account.Minecraft.Token != "session" {
		t.Errorf("a token that still holds is playable: %+v", notch)
	}
	if notch.Account.RefreshToken != "" {
		t.Error("a launcher account is never renewed, so no refresh token is kept")
	}
	if jeb.State != account.TokenExpired || jeb.Account.Minecraft == nil || jeb.Account.Minecraft.Token != "stale" {
		t.Errorf("an expired launcher account keeps its token: %+v", jeb)
	}
	if text := jeb.State.Text(jeb.Expired, prismNow); text != "token expired 2 days ago" {
		t.Errorf("state text = %q", text)
	}
}

func TestReadATLauncherTokenExpiry(t *testing.T) {
	for _, c := range []struct {
		name    string
		expires string
		want    account.State
		expired time.Time
	}{
		{"holds", "2026-09-21T00:00:00Z", account.Playable, time.Time{}},
		{"fractional seconds", "2026-09-21T00:00:00.1234567Z", account.Playable, time.Time{}},
		{"ran out", "2026-09-20T11:00:00Z", account.TokenExpired, time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)},
		{"undated", "", account.TokenExpired, time.Time{}},
		{"en-US text from an older file", "Sep 21, 2026, 12:00:00 AM", account.TokenExpired, time.Time{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writeATLauncherAccounts(t, `[`+atlauncherAccountJSON("069a79f44e9a4726a5befca90e38aaf5", "Notch", "s", c.expires)+`]`)
			got, errs := atlauncherEntry.Accounts(atlauncherEntry, dir, prismNow)
			if errs != nil || len(got) != 1 {
				t.Fatalf("read = %+v %v", got, errs)
			}
			if got[0].State != c.want || !got[0].Expired.Equal(c.expired) {
				t.Errorf("state = %q expired %v, want %q %v", got[0].State, got[0].Expired, c.want, c.expired)
			}
		})
	}
}

func TestReadATLauncherSilentAndCorrupt(t *testing.T) {
	if got, errs := atlauncherEntry.Accounts(atlauncherEntry, t.TempDir(), prismNow); got != nil || errs != nil {
		t.Errorf("no accounts file says nothing: %+v %v", got, errs)
	}
	if got, errs := atlauncherEntry.Accounts(atlauncherEntry, writeATLauncherAccounts(t, `null`), prismNow); got != nil || errs != nil {
		t.Errorf("a list ATLauncher saved empty says nothing: %+v %v", got, errs)
	}
	if _, errs := atlauncherEntry.Accounts(atlauncherEntry, writeATLauncherAccounts(t, `[{`), prismNow); len(errs) != 1 {
		t.Errorf("a file that doesn't parse warns: %v", errs)
	}
}
