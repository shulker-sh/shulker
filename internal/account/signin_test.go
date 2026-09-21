package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

var signInNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// fakeMSA stands in for the whole chain: the Microsoft device code and token endpoints, Xbox Live,
// XSTS, and Minecraft services.
type fakeMSA struct {
	server *httptest.Server
	// pending is how many polls answer authorization_pending before the sign-in takes.
	pending int
	// refused is the OAuth error the token endpoint answers with instead of a token.
	refused string
	// noProfile makes /minecraft/profile a 404, the way an account that owns no Java looks.
	noProfile bool
	// xerr is the XSTS refusal an account with no Xbox side gets.
	xerr int64
	// userToken is what Xbox Live hands out, and userNotAfter how long it says it lasts.
	userToken    string
	userNotAfter string
	// refresh is the refresh token the next grant rotates to.
	refresh string
	// grants records each token request's grant type and the refresh token it carried.
	grants  []string
	given   []string
	polls   int
	xboxHit int
	parties []string
}

func newFakeMSA(t *testing.T) *fakeMSA {
	t.Helper()
	f := &fakeMSA{
		userToken:    "user-token",
		userNotAfter: signInNow.Add(14 * 24 * time.Hour).Format(time.RFC3339),
		refresh:      "refresh-1",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/devicecode", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"device_code":      "device-code",
			"user_code":        "FTBNSQMV",
			"verification_uri": "https://www.microsoft.com/link",
			"expires_in":       900,
			"interval":         0,
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		grant := r.Form.Get("grant_type")
		f.grants = append(f.grants, grant)
		if given := r.Form.Get("refresh_token"); given != "" {
			f.given = append(f.given, given)
		}
		if grant == deviceGrant {
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
		writeJSON(w, map[string]any{"access_token": "access-1", "refresh_token": f.refresh, "expires_in": 3600})
	})
	mux.HandleFunc("/xbox/user/authenticate", func(w http.ResponseWriter, r *http.Request) {
		f.xboxHit++
		writeJSON(w, map[string]any{"Token": f.userToken, "NotAfter": f.userNotAfter})
	})
	mux.HandleFunc("/xsts/xsts/authorize", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RelyingParty string
			Properties   struct{ UserTokens []string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.parties = append(f.parties, body.RelyingParty)
		if f.xerr != 0 {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"XErr": f.xerr, "Message": ""})
			return
		}
		claims := map[string]any{"uhs": "1234567890123456789"}
		if body.RelyingParty == xboxParty {
			claims["gtg"], claims["xid"] = "Big Dog 42", "2533274812345678"
		}
		writeJSON(w, map[string]any{
			"Token":         "xsts-for-" + body.RelyingParty,
			"DisplayClaims": map[string]any{"xui": []any{claims}},
		})
	})
	mux.HandleFunc("/services/authentication/login_with_xbox", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IdentityToken string `json:"identityToken"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasPrefix(body.IdentityToken, "XBL3.0 x=1234567890123456789;") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"access_token": "minecraft-token", "expires_in": 86400})
	})
	mux.HandleFunc("/services/minecraft/profile", func(w http.ResponseWriter, r *http.Request) {
		if f.noProfile {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer minecraft-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"id": "069a79f4-44e9-4726-a5be-fca90e38aaf5", "name": "Notch"})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]any{"error": code, "error_description": code + " happened"})
}

func (f *fakeMSA) signIn() *SignIn {
	s := NewSignIn(fetch.New("test"))
	s.OAuthURL = f.server.URL + "/oauth"
	s.XboxURL = f.server.URL + "/xbox"
	s.XSTSURL = f.server.URL + "/xsts"
	s.ServicesURL = f.server.URL + "/services"
	s.Now = func() time.Time { return signInNow }
	return s
}

// login is the whole chain, the way `accounts login` runs it.
func (f *fakeMSA) login(t *testing.T) (Account, error) {
	t.Helper()
	s := f.signIn()
	ctx := context.Background()
	d, err := s.Start(ctx)
	if err != nil {
		return Account{}, err
	}
	if d.UserCode != "FTBNSQMV" || d.URL != "https://www.microsoft.com/link" {
		t.Fatalf("device = %+v", d)
	}
	tokens, err := s.Wait(ctx, d)
	if err != nil {
		return Account{}, err
	}
	return s.Complete(ctx, tokens)
}

func TestSignInStoresTheAccountAndItsTokens(t *testing.T) {
	f := newFakeMSA(t)
	f.pending = 2
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	if f.polls != 3 {
		t.Errorf("polls = %d, want the pending ones and the one that took", f.polls)
	}
	if a.Type != Microsoft || a.Name() != "Notch" || a.ID() != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Fatalf("account = %+v", a)
	}
	if a.RefreshToken != "refresh-1" || a.Minecraft.Token != "minecraft-token" {
		t.Errorf("tokens = %+v %+v", a.RefreshToken, a.Minecraft)
	}
	if want := signInNow.Add(24 * time.Hour).Format(time.RFC3339); a.Minecraft.ExpiresAt != want {
		t.Errorf("expiresAt = %q, want %q", a.Minecraft.ExpiresAt, want)
	}
	if a.Xbox.Token != "user-token" || a.Xbox.NotAfter != f.userNotAfter || a.Xbox.UserHash != "1234567890123456789" {
		t.Errorf("xbox = %+v", a.Xbox)
	}
	if a.State() != Playable {
		t.Errorf("state = %q", a.State())
	}
	// An ordinary sign-in never asks the Xbox relying party for anything.
	if len(f.parties) != 1 || f.parties[0] != minecraftParty {
		t.Errorf("relying parties = %q", f.parties)
	}
}

func TestSignInWithNoJavaProfileIsNamedByItsGamertag(t *testing.T) {
	f := newFakeMSA(t)
	f.noProfile = true
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	if a.Profile != nil {
		t.Fatalf("profile = %+v", a.Profile)
	}
	if a.Name() != "Big Dog 42" || a.ID() != "2533274812345678" {
		t.Errorf("account = %q %q", a.Name(), a.ID())
	}
	if a.State() != NoProfile {
		t.Errorf("state = %q", a.State())
	}
	if len(f.parties) != 2 || f.parties[1] != xboxParty {
		t.Errorf("relying parties = %q, want the gamertag call only on the 404 path", f.parties)
	}
}

func TestSignInDeclinedAndExpired(t *testing.T) {
	for _, c := range []struct{ refused, want string }{
		{"authorization_declined", "declined"},
		{"expired_token", "ran out"},
		{"bad_verification_code", "was refused"},
	} {
		t.Run(c.refused, func(t *testing.T) {
			f := newFakeMSA(t)
			f.refused = c.refused
			_, err := f.login(t)
			if out.CodeOf(err) != "sign-in-failed" || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v (%s)", err, out.CodeOf(err))
			}
		})
	}
}

func TestSignInWithoutAnXboxProfile(t *testing.T) {
	f := newFakeMSA(t)
	f.xerr = 2148916233
	_, err := f.login(t)
	if e := out.AsError(err); e.Code != "sign-in-failed" || !strings.Contains(e.Message, "no Xbox profile") || !strings.Contains(e.Help, "minecraft.net") {
		t.Fatalf("err = %v (%s)", err, out.CodeOf(err))
	}
}

func TestRenewUsesTheXboxTokenWhileItHolds(t *testing.T) {
	f := newFakeMSA(t)
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	grants, xboxHits := len(f.grants), f.xboxHit
	renewed, err := f.signIn().Renew(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.grants) != grants || f.xboxHit != xboxHits {
		t.Errorf("a renewal with a live Xbox token asked Microsoft again: grants %q, xbox %d", f.grants, f.xboxHit)
	}
	if renewed.RefreshToken != a.RefreshToken || renewed.Minecraft.Token != "minecraft-token" {
		t.Errorf("renewed = %+v", renewed)
	}
}

func TestRenewRotatesTheRefreshToken(t *testing.T) {
	f := newFakeMSA(t)
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	// The Xbox token has run out, so the chain starts at the refresh token again.
	a.Xbox.NotAfter = signInNow.Add(-time.Minute).Format(time.RFC3339)
	f.refresh = "refresh-2"
	renewed, err := f.signIn().Renew(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.RefreshToken != "refresh-2" {
		t.Errorf("refresh token = %q, want the rotated one", renewed.RefreshToken)
	}
	if len(f.given) != 1 || f.given[0] != "refresh-1" {
		t.Errorf("the renewal sent %q, want the one it had", f.given)
	}
	if f.grants[len(f.grants)-1] != "refresh_token" {
		t.Errorf("grants = %q", f.grants)
	}
}

func TestRenewWithARevokedRefreshTokenIsSignInExpired(t *testing.T) {
	f := newFakeMSA(t)
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	a.Xbox = nil
	f.refused = "invalid_grant"
	_, err = f.signIn().Renew(context.Background(), a)
	if out.CodeOf(err) != "account-sign-in-expired" {
		t.Fatalf("err = %v (%s)", err, out.CodeOf(err))
	}
	e := out.AsError(err)
	if len(e.Rows) != 1 || e.Rows[0].Text != "shulker accounts login" {
		t.Errorf("rows = %+v", e.Rows)
	}
}

func TestRenewWithNoRefreshTokenIsSignInExpired(t *testing.T) {
	s := newFakeMSA(t).signIn()
	a := Account{Type: Microsoft, Profile: &Profile{ID: "id", Name: "Notch"}}
	if _, err := s.Renew(context.Background(), a); out.CodeOf(err) != "account-sign-in-expired" {
		t.Fatalf("err = %v (%s)", err, out.CodeOf(err))
	}
}

func TestFreshReusesATokenWithOverAnHourLeft(t *testing.T) {
	for _, c := range []struct {
		name  string
		in    time.Duration
		fresh bool
	}{
		{"a day", 24 * time.Hour, true},
		{"just over the hour", time.Hour + time.Minute, true},
		{"within the hour", 59 * time.Minute, false},
		{"already expired", -time.Minute, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := Account{Minecraft: &Minecraft{Token: "t", ExpiresAt: signInNow.Add(c.in).Format(time.RFC3339)}}
			if got := a.IsFresh(signInNow); got != c.fresh {
				t.Errorf("IsFresh = %v, want %v", got, c.fresh)
			}
		})
	}
	if (Account{}).IsFresh(signInNow) {
		t.Error("an account with no token is never fresh")
	}
	if (Account{Minecraft: &Minecraft{Token: "t"}}).IsFresh(signInNow) {
		t.Error("a token with no expiry is never fresh")
	}
}
