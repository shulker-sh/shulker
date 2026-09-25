package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/account"
)

var prismNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func writePrism(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PrismAccountsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadPrismTakesMSAAndOfflineAccounts(t *testing.T) {
	dir := writePrism(t, `{
	  "formatVersion": 3,
	  "accounts": [
	    {
	      "type": "MSA",
	      "msa": {"token": "m", "refresh_token": "r", "exp": 1790000000},
	      "utoken": {"token": "u"},
	      "xrp-mc": {"token": "x"},
	      "ygg": {"iat": 1789862400, "exp": 1789948800, "token": "session"},
	      "profile": {"id": "069a79f4-44e9-4726-a5be-fca90e38aaf5", "name": "Notch"}
	    },
	    {
	      "type": "Offline",
	      "ygg": {"token": "0"},
	      "profile": {"id": "5627dd98-e6be-3c21-b8a8-e92344183641", "name": "Steve"}
	    }
	  ]
	}`)

	got, err := readPrismAccounts(prismEntry, dir, prismNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("accounts = %+v", got)
	}
	notch := got[0]
	if notch.Name != "Notch" || notch.Source != prismEntry.Name || notch.Group != account.GroupLauncher {
		t.Errorf("msa row = %+v", notch)
	}
	if notch.State != account.Playable {
		t.Errorf("a token that still holds is playable: %+v", notch)
	}
	if notch.Account.Minecraft == nil || notch.Account.Minecraft.Token != "session" {
		t.Errorf("the session token is what a launch needs: %+v", notch.Account.Minecraft)
	}
	if notch.Account.RefreshToken != "" {
		t.Error("a launcher account is never renewed, so no refresh token is kept")
	}
	steve := got[1]
	if steve.State != account.OfflineOnly || steve.Source != prismEntry.Name || steve.Group != account.GroupLauncher {
		t.Errorf("offline row = %+v", steve)
	}
}

func TestReadPrismTokenExpiry(t *testing.T) {
	// Prism dates a token by its exp, and by a day after it was issued when the file carries none.
	for _, c := range []struct {
		name    string
		ygg     string
		want    account.State
		expired time.Time
	}{
		{"holds", `{"token": "s", "exp": 1789948800}`, account.Playable, time.Time{}},
		{"ran out", `{"token": "s", "exp": 1789732800}`, account.TokenExpired, time.Unix(1789732800, 0)},
		{"a day after it was issued", `{"token": "s", "iat": 1789862400}`, account.Playable, time.Time{}},
		{"issued two days ago", `{"token": "s", "iat": 1789732800}`, account.TokenExpired, time.Unix(1789819200, 0)},
		{"no token", `{"exp": 1789948800}`, account.TokenExpired, time.Time{}},
		{"empty token", `{"token": "", "exp": 1789948800}`, account.TokenExpired, time.Time{}},
		{"undated", `{"token": "s"}`, account.TokenExpired, time.Time{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writePrism(t, `{"formatVersion":3,"accounts":[{"type":"MSA","ygg":`+c.ygg+
				`,"profile":{"id":"069a79f4-44e9-4726-a5be-fca90e38aaf5","name":"Notch"}}]}`)
			got, err := readPrismAccounts(prismEntry, dir, prismNow)
			if err != nil || len(got) != 1 {
				t.Fatalf("read = %+v %v", got, err)
			}
			if got[0].State != c.want {
				t.Errorf("state = %q, want %q", got[0].State, c.want)
			}
			if !got[0].Expired.Equal(c.expired) {
				t.Errorf("expired = %v, want %v", got[0].Expired, c.expired)
			}
		})
	}
}

func TestReadPrismExpiredTokenStaysUsable(t *testing.T) {
	dir := writePrism(t, `{"formatVersion":3,"accounts":[{"type":"MSA","ygg":{"token":"stale","exp":1789732800},
	  "profile":{"id":"069a79f4-44e9-4726-a5be-fca90e38aaf5","name":"Notch"}}]}`)
	got, err := readPrismAccounts(prismEntry, dir, prismNow)
	if err != nil || len(got) != 1 {
		t.Fatalf("read = %+v %v", got, err)
	}
	if got[0].Account.Minecraft == nil || got[0].Account.Minecraft.Token != "stale" {
		t.Error("an expired launcher account keeps its token: a launch uses it and warns")
	}
	if text := got[0].State.Text(got[0].Expired, prismNow); text != "token expired 2 days ago" {
		t.Errorf("state text = %q", text)
	}
}

func TestReadPrismSilentCases(t *testing.T) {
	// A launcher signed out of, one never installed, and an entry that names no profile: each
	// contributes nothing and says nothing.
	for _, c := range []struct{ name, body string }{
		{"no accounts", `{"formatVersion":3,"accounts":[]}`},
		{"no accounts key", `{"formatVersion":3}`},
		{"no profile", `{"formatVersion":3,"accounts":[{"type":"MSA","ygg":{"token":"s"},"username":"someone@example.com"}]}`},
		{"profile without an id", `{"formatVersion":3,"accounts":[{"type":"MSA","profile":{"name":"Notch"}}]}`},
		{"unknown type", `{"formatVersion":3,"accounts":[{"type":"Yggdrasil","profile":{"id":"u1","name":"Notch"}}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := readPrismAccounts(prismEntry, writePrism(t, c.body), prismNow)
			if err != nil || len(got) != 0 {
				t.Fatalf("read = %+v %v", got, err)
			}
		})
	}
}

func TestReadPrismMissingDirectoryIsSilent(t *testing.T) {
	got, err := readPrismAccounts(prismEntry, filepath.Join(t.TempDir(), "nowhere"), prismNow)
	if err != nil || got != nil {
		t.Fatalf("read = %+v %v", got, err)
	}
}

func TestReadPrismUnreadableFileNamesItself(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"broken json", `{"formatVersion":3,`, "unexpected end of JSON input"},
		{"a version shulker doesn't read", `{"formatVersion":4,"accounts":[]}`, "format version 4"},
		{"no version", `{"accounts":[]}`, "format version 0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writePrism(t, c.body)
			got, err := readPrismAccounts(prismEntry, dir, prismNow)
			if err == nil {
				t.Fatalf("read = %+v, want an error to warn with", got)
			}
			if !strings.Contains(err.Error(), filepath.Join(dir, PrismAccountsFile)) {
				t.Errorf("the warning names the file: %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q in it", err, c.want)
			}
		})
	}
}
