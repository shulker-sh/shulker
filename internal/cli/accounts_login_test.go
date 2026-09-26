package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/play"
)

// fakeMSA stands in for the whole sign-in chain: Microsoft's device code and token endpoints, Xbox
// Live, XSTS, and Minecraft services. Every token it hands out carries the key of the account it
// belongs to, so a renewal months later reaches the same account as the sign-in did.
type fakeMSA struct {
	accounts map[string]*fakeAccount
	// current is the account the next sign-in is for; a renewal finds its own by its token.
	current   *fakeAccount
	byRefresh map[string]*fakeAccount
	// pending is how many polls answer authorization_pending before the sign-in takes.
	pending int
	// refused is the OAuth error the token endpoint answers with instead of a token.
	refused   string
	expiresIn int
	notAfter  time.Time
	polls     int
	renewals  int
	parties   []string
	// scopes is what every request asked for, which is all shulker is ever allowed to ask for.
	scopes []string
}

type fakeAccount struct {
	key, name, id, gamertag, xuid string
	// noProfile makes /minecraft/profile a 404, the way an account that owns no Java looks.
	noProfile bool
	issues    int
}

// signsIn is the account the next `accounts login` signs in, made the first time it is named.
func (f *fakeMSA) signsIn(key, name, id string) *fakeAccount {
	a, ok := f.accounts[key]
	if !ok {
		a = &fakeAccount{key: key, name: name, id: id, gamertag: name, xuid: "xuid-" + key}
		f.accounts[key] = a
	}
	f.current = a
	return a
}

func (f *fakeMSA) byToken(token string) *fakeAccount {
	_, key, _ := strings.Cut(token, "-")
	return f.accounts[key]
}

// issue is the refresh token the account rotates to, which is a new one every time.
func (f *fakeMSA) issue(a *fakeAccount) string {
	a.issues++
	token := fmt.Sprintf("refresh-%s-%d", a.key, a.issues)
	f.byRefresh[token] = a
	return token
}

func (h *harness) fakeSignIn(mux *http.ServeMux) *fakeMSA {
	f := &fakeMSA{
		accounts:  map[string]*fakeAccount{},
		byRefresh: map[string]*fakeAccount{},
		expiresIn: 86400,
		notAfter:  time.Now().Add(14 * 24 * time.Hour),
	}
	notch := f.signsIn("notch", "Notch", notchID)
	notch.gamertag, notch.xuid = "Big Dog 42", gamertagXID
	mux.HandleFunc("/msa/oauth/devicecode", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.scopes = append(f.scopes, r.Form.Get("scope"))
		writeJSON(w, map[string]any{
			"device_code":      "device-code",
			"user_code":        "FTBNSQMV",
			"verification_uri": "https://www.microsoft.com/link",
			"expires_in":       900,
			"interval":         0,
		})
	})
	mux.HandleFunc("/msa/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		a := f.current
		if scope := r.Form.Get("scope"); scope != "" {
			f.scopes = append(f.scopes, scope)
		}
		if given := r.Form.Get("refresh_token"); given != "" {
			f.renewals++
			if a = f.byRefresh[given]; a == nil {
				writeOAuthError(w, "invalid_grant")
				return
			}
		} else {
			f.polls++
			if f.polls <= f.pending {
				writeOAuthError(w, "authorization_pending")
				return
			}
		}
		if f.refused != "" {
			writeOAuthError(w, f.refused)
			return
		}
		writeJSON(w, map[string]any{"access_token": "access-" + a.key, "refresh_token": f.issue(a), "expires_in": 3600})
	})
	mux.HandleFunc("/msa/xbox/user/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Properties struct{ RpsTicket string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		a := f.byToken(strings.TrimPrefix(body.Properties.RpsTicket, "d="))
		if a == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"Token": "user-" + a.key, "NotAfter": f.notAfter.Format(time.RFC3339)})
	})
	mux.HandleFunc("/msa/xsts/xsts/authorize", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RelyingParty string
			Properties   struct{ UserTokens []string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.parties = append(f.parties, body.RelyingParty)
		a := f.byToken(body.Properties.UserTokens[0])
		if a == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		claims := map[string]any{"uhs": "uhs-" + a.key}
		if strings.Contains(body.RelyingParty, "xboxlive.com") {
			claims["gtg"], claims["xid"] = a.gamertag, a.xuid
		}
		writeJSON(w, map[string]any{"Token": "xsts-" + a.key, "DisplayClaims": map[string]any{"xui": []any{claims}}})
	})
	mux.HandleFunc("/msa/services/authentication/login_with_xbox", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IdentityToken string `json:"identityToken"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		hash, _, _ := strings.Cut(strings.TrimPrefix(body.IdentityToken, "XBL3.0 x="), ";")
		a := f.byToken(hash)
		if a == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"access_token": "mc-" + a.key, "expires_in": f.expiresIn})
	})
	mux.HandleFunc("/msa/services/minecraft/profile", func(w http.ResponseWriter, r *http.Request) {
		a := f.byToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if a == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if a.noProfile {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"id": a.id, "name": a.name})
	})
	return f
}

func (f *fakeMSA) signIn(client *fetch.Client, base string) *account.SignIn {
	s := account.NewSignIn(client)
	s.OAuthURL = base + "/msa/oauth"
	s.XboxURL = base + "/msa/xbox"
	s.XSTSURL = base + "/msa/xsts"
	s.ServicesURL = base + "/msa/services"
	return s
}

func writeOAuthError(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]any{"error": code, "error_description": code})
}

func readAccountStore(t *testing.T, h *harness) account.Store {
	t.Helper()
	store, err := account.Load(account.Path(h.config))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAccountsLoginStoresTheAccountAndItsTokens(t *testing.T) {
	h := newHarness(t)
	h.msa.pending = 2
	stdout, stderr := h.mustRunStderr(t, "accounts", "login")
	for _, want := range []string{"Sign in at https://www.microsoft.com/link with this code:", "\n    FTBNSQMV\n"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the sign-in prompt is missing %q:\n%s", want, stderr)
		}
	}
	if !strings.Contains(stdout, "✔ Signed in as Notch ("+notchID+")") {
		t.Errorf("login result: %s", stdout)
	}
	if !strings.Contains(stdout, "Make it the default account:\n    $ shulker accounts use Notch") {
		t.Errorf("login should leave the default alone and hint at it: %s", stdout)
	}
	// The scope is the whole of what shulker asks for: an email address would cost a consent
	// screen to name what the gamertag names for free.
	for _, scope := range h.msa.scopes {
		if scope != "XboxLive.signin offline_access" {
			t.Errorf("scope = %q", scope)
		}
	}
	if h.msa.polls != 3 {
		t.Errorf("polls = %d, want the pending ones and the one that took", h.msa.polls)
	}
	store := readAccountStore(t, h)
	if len(store.Accounts) != 1 {
		t.Fatalf("store = %+v", store.Accounts)
	}
	a := store.Accounts[0]
	if a.Type != account.Microsoft || a.Profile.Name != "Notch" || a.Profile.ID != notchID {
		t.Fatalf("account = %+v", a)
	}
	if a.RefreshToken != "refresh-notch-1" || a.Minecraft.Token != "mc-notch" {
		t.Errorf("tokens = %q %+v", a.RefreshToken, a.Minecraft)
	}
	if !a.IsFresh(time.Now()) {
		t.Errorf("a token a day out should be fresh: %+v", a.Minecraft)
	}
	if a.Xbox.Token != "user-notch" || a.Xbox.UserHash != "uhs-notch" {
		t.Errorf("xbox = %+v", a.Xbox)
	}
	// Nothing is the default until the player says so.
	if stdout := h.mustRun(t, "config", "get", "--json"); strings.Contains(stdout, "default") {
		t.Errorf("login set a default account: %s", stdout)
	}
	// A second run signs a second account in beside the first.
	h.msa.signsIn("dinnerbone", "Dinnerbone", dinnerbone)
	h.mustRun(t, "accounts", "login")
	if store := readAccountStore(t, h); len(store.Accounts) != 2 {
		t.Fatalf("a second login should add an account: %+v", store.Accounts)
	}
	if list := h.mustRun(t, "accounts"); !strings.Contains(list, "Notch") || !strings.Contains(list, "Dinnerbone") {
		t.Errorf("accounts = %s", list)
	}
}

func TestAccountsLoginAgainUpdatesTheSameAccount(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "accounts", "login")
	h.mustRun(t, "accounts", "login")
	store := readAccountStore(t, h)
	if len(store.Accounts) != 1 {
		t.Fatalf("signing in again should update the entry: %+v", store.Accounts)
	}
	if store.Accounts[0].RefreshToken != "refresh-notch-2" {
		t.Errorf("refresh token = %q, want the one from the second sign-in", store.Accounts[0].RefreshToken)
	}
}

func TestAccountsLoginWithNoJavaProfile(t *testing.T) {
	h := newHarness(t)
	h.msa.current.noProfile = true
	stdout, stderr := h.mustRunStderr(t, "accounts", "login")
	if !strings.Contains(stdout, "✔ Signed in as Big Dog 42 ("+gamertagXID+")") {
		t.Errorf("login result: %s", stdout)
	}
	if !strings.Contains(stderr, "owns no Java profile") || !strings.Contains(stderr, "minecraft.net") {
		t.Errorf("an account with no profile needs the hint: %s", stderr)
	}
	if strings.Contains(stdout, "accounts use") {
		t.Errorf("an account that can't launch is no default to suggest: %s", stdout)
	}
	store := readAccountStore(t, h)
	if a := store.Accounts[0]; a.Profile != nil || a.Xbox.Gamertag != "Big Dog 42" || a.Xbox.XUID != gamertagXID {
		t.Fatalf("account = %+v %+v", a.Profile, a.Xbox)
	}
	if state := store.Accounts[0].State(); state != account.NoProfile {
		t.Errorf("state = %q", state)
	}
	// The gamertag costs one XSTS call against the Xbox relying party, and only on the 404 path.
	if len(h.msa.parties) != 2 || !strings.Contains(h.msa.parties[1], "xboxlive.com") {
		t.Errorf("relying parties = %q", h.msa.parties)
	}
	h.msa.parties = nil
	h.msa.current.noProfile = false
	h.mustRun(t, "accounts", "login")
	if len(h.msa.parties) != 1 {
		t.Errorf("an ordinary sign-in asked for more than the Minecraft party: %q", h.msa.parties)
	}
	// The entry it had before it owned Java is the one that gets the profile.
	if store := readAccountStore(t, h); len(store.Accounts) != 1 || store.Accounts[0].ID() != notchID {
		t.Fatalf("store = %+v", store.Accounts)
	}
}

func TestAccountsLoginUseSwitchesTheDefault(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "accounts", "login", "--use")
	if !strings.Contains(stdout, "✔ Signed in as Notch, now the default account") {
		t.Errorf("--use result: %s", stdout)
	}
	if !strings.Contains(h.mustRun(t, "accounts"), "✔  Notch") {
		t.Error("the account it signed in should be marked the default")
	}
}

func TestAccountsLoginDeclined(t *testing.T) {
	h := newHarness(t)
	h.msa.refused = "authorization_declined"
	code, stdout, _ := h.run(t, "accounts", "login", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "sign-in-failed" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if store := readAccountStore(t, h); len(store.Accounts) != 0 {
		t.Errorf("a declined sign-in stored %+v", store.Accounts)
	}
}

func TestAccountsLogout(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "accounts", "login")

	// Confirming is the default, and a run that can't ask needs the flag that answers it.
	code, stdout, _ := h.run(t, "accounts", "logout", "Notch", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Help, "--yes") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if len(readAccountStore(t, h).Accounts) != 1 {
		t.Fatal("nothing should be signed out without an answer")
	}

	stdout = h.mustRun(t, "accounts", "logout", "Notch", "--yes")
	if !strings.Contains(stdout, "✔ Signed out Notch ("+notchID+")") {
		t.Errorf("logout result: %s", stdout)
	}
	if len(readAccountStore(t, h).Accounts) != 0 {
		t.Error("logout left the account behind")
	}
}

func TestAccountsLogoutTakesTheDefaultAndReseatsIt(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "accounts", "login", "--use")
	h.msa.signsIn("dinnerbone", "Dinnerbone", dinnerbone)
	h.mustRun(t, "accounts", "login")

	// With no name it is the default account that signs out, and the one playable account left
	// takes its place.
	stdout := h.mustRun(t, "accounts", "logout", "--yes")
	if !strings.Contains(stdout, "Signed out Notch") || !strings.Contains(stdout, "Dinnerbone is the default account now") {
		t.Fatalf("logout result: %s", stdout)
	}
	if !strings.Contains(h.mustRun(t, "accounts"), "✔  Dinnerbone") {
		t.Error("the account left should be the default")
	}
	stdout = h.mustRun(t, "accounts", "logout", "--yes")
	if !strings.Contains(stdout, "No default account now") {
		t.Errorf("the last logout should leave no default: %s", stdout)
	}
	if _, _, stderr := h.run(t, "config", "get", "accounts.default", "--json"); strings.Contains(stderr, dinnerbone) {
		t.Error("accounts.default still names an account that is gone")
	}
}

func TestAccountsLogoutNamesRemoveForAnOfflineAccount(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, offlineAccount("Steve", steveID))
	code, stdout, _ := h.run(t, "accounts", "logout", "Steve", "--yes", "--json")
	e := failureCode(t, stdout)
	if code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Help, "accounts remove Steve") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestAccountsRefreshRenewsOwnAccounts(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "accounts", "login")
	// The stored Xbox token has run out, so a renewal starts at the refresh token again.
	store := readAccountStore(t, h)
	store.Accounts[0].Xbox.NotAfter = time.Now().Add(-time.Hour).Format(time.RFC3339)
	if err := account.Save(account.Path(h.config), store); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := h.mustRunStderr(t, "accounts", "refresh")
	if !strings.Contains(stdout, "✔ Renewed Notch's sign-in") {
		t.Errorf("refresh result: %s", stdout)
	}
	if !strings.Contains(stderr, "Renewed Notch") {
		t.Errorf("each account gets its own step line: %s", stderr)
	}
	if h.msa.renewals != 1 {
		t.Errorf("renewals = %d", h.msa.renewals)
	}
	if got := readAccountStore(t, h).Accounts[0].RefreshToken; got != "refresh-notch-2" {
		t.Errorf("refresh token = %q, want the rotated one", got)
	}
}

func TestAccountsRefreshKeepsGoingPastAnExpiredSignIn(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "accounts", "login")
	h.msa.signsIn("dinnerbone", "Dinnerbone", dinnerbone)
	h.mustRun(t, "accounts", "login")
	// Notch's sign-in is gone; Dinnerbone's still works.
	store := readAccountStore(t, h)
	for i, a := range store.Accounts {
		store.Accounts[i].Xbox.NotAfter = time.Now().Add(-time.Hour).Format(time.RFC3339)
		if a.ID() == notchID {
			store.Accounts[i].RefreshToken = ""
		}
	}
	if err := account.Save(account.Path(h.config), store); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := h.mustRunStderr(t, "accounts", "refresh")
	if !strings.Contains(stdout, "✔ Renewed Dinnerbone's sign-in (1 couldn't be renewed)") {
		t.Errorf("refresh result: %s", stdout)
	}
	if !strings.Contains(stderr, "Notch's Microsoft sign-in has expired") || !strings.Contains(stderr, "shulker accounts login") {
		t.Errorf("the expired account needs a warning with its fix: %s", stderr)
	}
}

func TestAccountsRefreshMissingProfile(t *testing.T) {
	h := newHarness(t)
	noProfile := h.msa.current
	noProfile.noProfile = true
	h.mustRun(t, "accounts", "login")
	h.msa.signsIn("dinnerbone", "Dinnerbone", dinnerbone)
	h.mustRun(t, "accounts", "login")

	refreshed := func(args ...string) []accountRow {
		t.Helper()
		var rows []accountRow
		if err := json.Unmarshal(h.runSetting(t, 0, append([]string{"accounts", "refresh"}, args...)...).Data, &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := refreshed("--missing-profile", "--json")
	if len(rows) != 1 || rows[0].Name != "Big Dog 42" {
		t.Fatalf("--missing-profile renewed %+v", rows)
	}
	// Names and the filter are an AND: a named account that owns a profile is not one of them.
	if rows := refreshed("Dinnerbone", "--missing-profile", "--json"); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	// Which is what the filter is for: the account bought Java, and a refresh picks up the
	// profile it now has, under the entry it was stored in without one.
	noProfile.noProfile = false
	if rows := refreshed("--missing-profile", "--json"); len(rows) != 1 || rows[0].Name != "Notch" || rows[0].State != account.Playable {
		t.Fatalf("rows = %+v", rows)
	}
	if store := readAccountStore(t, h); len(store.Accounts) != 2 {
		t.Fatalf("store = %+v", store.Accounts)
	}
}

func TestAccountsRefreshRejectsAnAccountItDidNotSignIn(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, offlineAccount("Steve", steveID))
	code, stdout, _ := h.run(t, "accounts", "refresh", "Steve", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

// sessionFor is what a launch signs in with, asked for directly so the warning and the saved
// renewal can be checked without a game.
func sessionFor(t *testing.T, h *harness, name string) (account.Account, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := h.newApp(&stdout, &stderr)
	r, err := a.selectAccount(name)
	if err != nil {
		return account.Account{}, "", err
	}
	pe, err := a.playEnv()
	if err != nil {
		return account.Account{}, "", err
	}
	signed, err := play.Session(context.Background(), pe, r)
	return signed, stderr.String(), err
}

// The prompt is the one thing a sign-in shows, so it is checked as it is painted: the page cyan
// like anything else you can act on, an OSC 8 link on the terminals that follow them, and the code
// bold on its own line.
func TestSignInPromptIsStyled(t *testing.T) {
	h := newHarness(t)
	device := account.Device{UserCode: "FTBNSQMV", URL: "https://www.microsoft.com/link"}
	var stdout, stderr bytes.Buffer
	a := h.newApp(&stdout, &stderr)
	a.printer.ErrTheme = out.Theme{HasColor: true, HasLinks: true, GreyIndex: out.GreyFallback}
	a.showDeviceCode(device)
	painted := stderr.String()
	for _, want := range []string{
		"\x1b]8;;https://www.microsoft.com/link\x1b\\",
		"\x1b[36m\x1b[1mhttps://www.microsoft.com/link\x1b[0m",
		"\n  \x1b[38;5;244mSign in at ",
		"\n    \x1b[1mFTBNSQMV\x1b[0m\n",
	} {
		if !strings.Contains(painted, want) {
			t.Errorf("the sign-in prompt is missing %q:\n%q", want, painted)
		}
	}
	stderr.Reset()
	a = h.newApp(&stdout, &stderr)
	a.showDeviceCode(device)
	if plain := stderr.String(); strings.Contains(plain, "\x1b") {
		t.Errorf("off a terminal the prompt carries no escapes: %q", plain)
	}
}
