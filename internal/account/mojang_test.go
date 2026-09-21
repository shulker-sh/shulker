package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var mojangNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

const (
	notchUUID = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	jebUUID   = "853c80ef-3c37-49fd-aa49-938b674adae6"
)

func writeMojang(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mojangEntry(local, id, name, token, expires string) string {
	return `"` + local + `": {
	  "accessToken": "` + token + `",
	  "accessTokenExpiresAt": "` + expires + `",
	  "username": "someone@example.com",
	  "localId": "` + local + `",
	  "remoteId": "` + id + `",
	  "persistent": true,
	  "type": "Xbox",
	  "minecraftProfile": {"id": "` + id + `", "name": "` + name + `"}
	}`
}

func mojangBody(entries ...string) string {
	return `{
	  "accounts": {` + strings.Join(entries, ",") + `},
	  "activeAccountLocalId": "one",
	  "mojangClientToken": "ct"
	}`
}

func TestReadMojangTakesAccountsFromBothFiles(t *testing.T) {
	dir := writeMojang(t, map[string]string{
		MojangFileName:      mojangBody(mojangEntry("one", notchUUID, "Notch", "session", "2026-09-21T12:00:00Z")),
		MojangStoreFileName: mojangBody(mojangEntry("two", jebUUID, "Jeb_", "store", "2026-09-21T12:00:00Z")),
	})

	got, errs := ReadMojang(dir, mojangNow)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("both files contribute their accounts: %+v", got)
	}
	jeb, notch := got[0], got[1]
	if jeb.Name != "Jeb_" || notch.Name != "Notch" {
		t.Fatalf("accounts = %+v", got)
	}
	if notch.Source != SourceMojang || notch.Group != GroupBorrowed || notch.State != Playable {
		t.Errorf("row = %+v", notch)
	}
	if notch.ID != notchUUID || notch.Account.Profile == nil || notch.Account.Profile.Name != "Notch" {
		t.Errorf("username and UUID come from minecraftProfile: %+v", notch)
	}
	if notch.Account.Type != Microsoft || notch.Account.Minecraft == nil || notch.Account.Minecraft.Token != "session" {
		t.Errorf("the session token is what a launch needs: %+v", notch.Account)
	}
	if notch.Account.RefreshToken != "" {
		t.Error("a borrowed account is never renewed, so no refresh token is kept")
	}
}

func TestReadMojangMergesAUUIDInBothFiles(t *testing.T) {
	// Username and UUID are equal by construction, so the later token is all the choice decides.
	for _, c := range []struct{ name, plain, store, want string }{
		{"the store file is fresher", "2026-09-18T12:00:00Z", "2026-09-21T12:00:00Z", "store"},
		{"the plain file is fresher", "2026-09-21T12:00:00Z", "2026-09-18T12:00:00Z", "session"},
		{"only the store file is dated", "", "2026-09-21T12:00:00Z", "store"},
		{"only the plain file is dated", "2026-09-21T12:00:00Z", "", "session"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writeMojang(t, map[string]string{
				MojangFileName:      mojangBody(mojangEntry("one", notchUUID, "Notch", "session", c.plain)),
				MojangStoreFileName: mojangBody(mojangEntry("two", notchUUID, "Notch", "store", c.store)),
			})
			got, errs := ReadMojang(dir, mojangNow)
			if len(errs) != 0 || len(got) != 1 {
				t.Fatalf("one UUID is one account: %+v %v", got, errs)
			}
			if got[0].Account.Minecraft == nil || got[0].Account.Minecraft.Token != c.want {
				t.Errorf("token = %+v, want %q", got[0].Account.Minecraft, c.want)
			}
		})
	}
}

func TestReadMojangMergesAUUIDWrittenTwoWays(t *testing.T) {
	dir := writeMojang(t, map[string]string{
		MojangFileName:      mojangBody(mojangEntry("one", notchUUID, "Notch", "session", "2026-09-21T12:00:00Z")),
		MojangStoreFileName: mojangBody(mojangEntry("two", strings.ReplaceAll(notchUUID, "-", ""), "Notch", "store", "2026-09-22T12:00:00Z")),
	})
	got, errs := ReadMojang(dir, mojangNow)
	if len(errs) != 0 || len(got) != 1 {
		t.Fatalf("a UUID dashed in one file and not the other is one account: %+v %v", got, errs)
	}
}

func TestReadMojangTokenExpiry(t *testing.T) {
	for _, c := range []struct {
		name, token, expires string
		want                 State
		expired              time.Time
	}{
		{"holds", "session", "2026-09-21T12:00:00Z", Playable, time.Time{}},
		{"fractional seconds", "session", "2026-09-21T12:00:00.0000000Z", Playable, time.Time{}},
		{"ran out", "session", "2026-09-18T12:00:00Z", TokenExpired, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)},
		{"no expiry", "session", "", TokenExpired, time.Time{}},
		{"unparseable expiry", "session", "yesterday", TokenExpired, time.Time{}},
		{"no token", "", "2026-09-21T12:00:00Z", TokenExpired, time.Time{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writeMojang(t, map[string]string{
				MojangFileName: mojangBody(mojangEntry("one", notchUUID, "Notch", c.token, c.expires)),
			})
			got, errs := ReadMojang(dir, mojangNow)
			if len(errs) != 0 || len(got) != 1 {
				t.Fatalf("an account with a profile is never skipped: %+v %v", got, errs)
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

func TestReadMojangExpiredTokenStaysUsable(t *testing.T) {
	dir := writeMojang(t, map[string]string{
		MojangFileName: mojangBody(mojangEntry("one", notchUUID, "Notch", "stale", "2026-09-18T12:00:00Z")),
	})
	got, errs := ReadMojang(dir, mojangNow)
	if len(errs) != 0 || len(got) != 1 {
		t.Fatalf("read = %+v %v", got, errs)
	}
	if got[0].Account.Minecraft == nil || got[0].Account.Minecraft.Token != "stale" {
		t.Error("an expired borrowed account keeps its token: a launch uses it and warns")
	}
	if text := got[0].State.Text(got[0].Expired, mojangNow); text != "token expired 2 days ago" {
		t.Errorf("state text = %q", text)
	}
}

func TestReadMojangSilentCases(t *testing.T) {
	// A launcher signed out of, and an entry that names no Java profile: each contributes
	// nothing and says nothing.
	for _, c := range []struct{ name, body string }{
		{"signed out", `{"accounts":{},"activeAccountLocalId":""}`},
		{"no accounts key", `{"mojangClientToken":"ct"}`},
		{"no profile", `{"accounts":{"one":{"accessToken":"s","username":"someone@example.com"}}}`},
		{"profile without an id", `{"accounts":{"one":{"minecraftProfile":{"name":"Notch"}}}}`},
		{"profile without a name", `{"accounts":{"one":{"minecraftProfile":{"id":"` + notchUUID + `"}}}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writeMojang(t, map[string]string{MojangFileName: c.body})
			got, errs := ReadMojang(dir, mojangNow)
			if len(errs) != 0 || len(got) != 0 {
				t.Fatalf("read = %+v %v", got, errs)
			}
		})
	}
}

func TestReadMojangMissingDirectoryIsSilent(t *testing.T) {
	got, errs := ReadMojang(filepath.Join(t.TempDir(), "nowhere"), mojangNow)
	if len(errs) != 0 || got != nil {
		t.Fatalf("read = %+v %v", got, errs)
	}
}

func TestReadMojangSkipsOnlyTheFileItCannotRead(t *testing.T) {
	dir := writeMojang(t, map[string]string{
		MojangFileName:      `{"accounts":{`,
		MojangStoreFileName: mojangBody(mojangEntry("two", jebUUID, "Jeb_", "store", "2026-09-21T12:00:00Z")),
	})
	got, errs := ReadMojang(dir, mojangNow)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want the one file that didn't parse", errs)
	}
	if !strings.Contains(errs[0].Error(), filepath.Join(dir, MojangFileName)) {
		t.Errorf("the warning names the file: %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "unexpected end of JSON input") {
		t.Errorf("the warning carries what went wrong: %v", errs[0])
	}
	if len(got) != 1 || got[0].Name != "Jeb_" {
		t.Errorf("the other file in the directory still loads: %+v", got)
	}
}

func TestReadMojangOpensNeitherEntitlementsNorCredentials(t *testing.T) {
	// Garbage in every file beside the two account lists: opening any of them would fail, and
	// the ownership proof is the Java profile rather than an entitlement.
	dir := writeMojang(t, map[string]string{
		MojangFileName:                                 mojangBody(mojangEntry("one", notchUUID, "Notch", "session", "2026-09-21T12:00:00Z")),
		"launcher_entitlements.json":                   "not json",
		"launcher_entitlements_microsoft_store.json":   "not json",
		"launcher_msa_credentials.bin":                 "not json",
		"launcher_msa_credentials_microsoft_store.bin": "not json",
	})
	got, errs := ReadMojang(dir, mojangNow)
	if len(errs) != 0 || len(got) != 1 || got[0].State != Playable {
		t.Fatalf("read = %+v %v", got, errs)
	}
}
