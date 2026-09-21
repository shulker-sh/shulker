package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

// Shulker's own Azure app and the scope it asks for. Nothing beyond XboxLive.signin and
// offline_access is asked: openid email would charge every player a consent screen to name the
// rare account that owns no Java profile, and the gamertag names that one for free.
const (
	ClientID = "2d8d4657-91f0-4797-a9b6-0eae06c4849e"
	Scope    = "XboxLive.signin offline_access"
)

// The endpoints of the five-step chain, each overridable so a test can stand in for it.
const (
	OAuthURL    = "https://login.microsoftonline.com/consumers/oauth2/v2.0"
	XboxURL     = "https://user.auth.xboxlive.com"
	XSTSURL     = "https://xsts.auth.xboxlive.com"
	ServicesURL = "https://api.minecraftservices.com"
)

// The two XSTS relying parties. The Minecraft one is what a session token is for, and it yields
// only the user hash; the Xbox one yields the gamertag and the Xbox user id, under the same scope.
const (
	minecraftParty = "rp://api.minecraftservices.com/"
	xboxParty      = "http://xboxlive.com"
)

const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

// renewWithin is how close to expiry a stored Minecraft token stops being reused: renewing takes
// four requests, which is worth doing before a launch rather than in the middle of one.
const renewWithin = time.Hour

// SignIn runs the Microsoft sign-in chain: the device code the player types, the Xbox and XSTS
// tokens it buys, and the Minecraft session token and profile at the end of it.
type SignIn struct {
	Client      *fetch.Client
	ClientID    string
	OAuthURL    string
	XboxURL     string
	XSTSURL     string
	ServicesURL string
	// Now stands in for the clock, so a test can say what a stored expiry means.
	Now func() time.Time
}

func NewSignIn(c *fetch.Client) *SignIn {
	return &SignIn{Client: c, ClientID: ClientID, OAuthURL: OAuthURL, XboxURL: XboxURL, XSTSURL: XSTSURL, ServicesURL: ServicesURL}
}

func (s *SignIn) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Device is what a sign-in asks of the player: a page to open and a code to type there.
type Device struct {
	UserCode string `json:"userCode"`
	URL      string `json:"url"`
	// code is what the poll sends, and never anything the player sees.
	code     string
	interval time.Duration
	expires  time.Time
}

// Tokens is what the Microsoft token endpoint answers with: the one the Xbox chain starts from,
// and the one that starts it again months later.
type Tokens struct {
	Access  string
	Refresh string
}

// Start asks Microsoft for a device code. Nothing is signed in until Wait returns.
func (s *SignIn) Start(ctx context.Context) (Device, error) {
	var res struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	form := url.Values{"client_id": {s.ClientID}, "scope": {Scope}}
	if err := s.Client.PostForm(ctx, s.OAuthURL+"/devicecode", form, &res); err != nil {
		return Device{}, signInFailed(err, "Microsoft wouldn't start a sign-in")
	}
	if res.DeviceCode == "" || res.UserCode == "" {
		return Device{}, out.Errorf("sign-in-failed", "Microsoft answered the sign-in request with no code")
	}
	d := Device{
		UserCode: res.UserCode,
		URL:      res.VerificationURI,
		code:     res.DeviceCode,
		interval: time.Duration(res.Interval) * time.Second,
		expires:  s.now().Add(time.Duration(res.ExpiresIn) * time.Second),
	}
	return d, nil
}

// Wait polls until the player finishes in the browser, and answers with the Microsoft tokens.
func (s *SignIn) Wait(ctx context.Context, d Device) (Tokens, error) {
	form := url.Values{"grant_type": {deviceGrant}, "client_id": {s.ClientID}, "device_code": {d.code}}
	for {
		t, err := s.token(ctx, form)
		var oa *oauthError
		if err == nil || !errors.As(err, &oa) {
			return t, err
		}
		switch oa.Code {
		case "authorization_pending":
		case "slow_down":
			d.interval += 5 * time.Second
		case "authorization_declined":
			return Tokens{}, out.Errorf("sign-in-failed", "the sign-in was declined")
		case "expired_token":
			return Tokens{}, out.Errorf("sign-in-failed", "the code %s ran out before the sign-in finished", d.UserCode)
		default:
			return Tokens{}, signInFailed(oa, "Microsoft refused the sign-in")
		}
		if s.now().After(d.expires) {
			return Tokens{}, out.Errorf("sign-in-failed", "the code %s ran out before the sign-in finished", d.UserCode)
		}
		if err := sleep(ctx, d.interval); err != nil {
			return Tokens{}, err
		}
	}
}

// Complete runs the rest of the chain: Xbox, XSTS, the Minecraft session token and the profile.
func (s *SignIn) Complete(ctx context.Context, t Tokens) (Account, error) {
	user, err := s.xboxUser(ctx, t.Access)
	if err != nil {
		return Account{}, err
	}
	return s.fromUser(ctx, user, t.Refresh)
}

// Renew signs an account in again without asking the player anything: from the Xbox user token
// while its NotAfter holds, and from the Microsoft refresh token otherwise. Microsoft rotates that
// token and leaves the old one working, so the new one replaces it rather than joining it.
func (s *SignIn) Renew(ctx context.Context, a Account) (Account, error) {
	if x := a.Xbox; x != nil && x.Token != "" && holds(x.NotAfter, s.now()) {
		renewed, err := s.fromUser(ctx, xboxUser{Token: x.Token, NotAfter: x.NotAfter}, a.RefreshToken)
		if err == nil {
			return renewed, nil
		}
		if fetch.IsNetwork(err) {
			return Account{}, err
		}
	}
	if a.RefreshToken == "" {
		return Account{}, SignInExpired.Error(a.Name())
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.ClientID},
		"scope":         {Scope},
		"refresh_token": {a.RefreshToken},
	}
	t, err := s.token(ctx, form)
	if err != nil {
		var oa *oauthError
		if errors.As(err, &oa) && oa.Code == "invalid_grant" {
			return Account{}, SignInExpired.Error(a.Name())
		}
		return Account{}, err
	}
	return s.Complete(ctx, t)
}

// Fresh reports whether a's Minecraft token has long enough left to launch on. One that expires
// within the hour counts as stale: the chain runs before the game starts, not during it.
func (a Account) Fresh(now time.Time) bool {
	return a.Minecraft != nil && a.Minecraft.Token != "" && holds(a.Minecraft.ExpiresAt, now.Add(renewWithin))
}

// fromUser turns an Xbox user token into a stored account: XSTS for the Minecraft relying party,
// the session token, and the profile — or, when there is none, the gamertag that names it.
func (s *SignIn) fromUser(ctx context.Context, user xboxUser, refresh string) (Account, error) {
	xsts, err := s.authorize(ctx, minecraftParty, user.Token)
	if err != nil {
		return Account{}, err
	}
	token, expires, err := s.minecraftToken(ctx, xsts.UserHash, xsts.Token)
	if err != nil {
		return Account{}, err
	}
	a := Account{
		Type:         Microsoft,
		RefreshToken: refresh,
		Xbox:         &Xbox{UserHash: xsts.UserHash, Token: user.Token, NotAfter: user.NotAfter},
		Minecraft:    &Minecraft{Token: token, ExpiresAt: expires},
	}
	profile, ok, err := s.profile(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if ok {
		a.Profile = &profile
		return a, nil
	}
	// No Java profile, so nothing so far names this account. One more XSTS, against the Xbox
	// relying party, costs no extra scope and answers with both a name and an id.
	xbox, err := s.authorize(ctx, xboxParty, user.Token)
	if err != nil {
		return Account{}, err
	}
	a.Xbox.XUID, a.Xbox.Gamertag = xbox.XUID, xbox.Gamertag
	return a, nil
}

// xboxUser is step two's answer: the token every later step is bought with, and how long it lasts.
type xboxUser struct {
	Token    string
	NotAfter string
}

func (s *SignIn) xboxUser(ctx context.Context, access string) (xboxUser, error) {
	body := map[string]any{
		"Properties": map[string]string{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + access,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	var res xboxAnswer
	if err := s.Client.PostJSON(ctx, s.XboxURL+"/user/authenticate", body, &res); err != nil {
		return xboxUser{}, signInFailed(err, "Xbox Live wouldn't take the Microsoft sign-in")
	}
	return xboxUser{Token: res.Token, NotAfter: res.NotAfter}, nil
}

// authorized is what an XSTS authorization yields, which depends on the relying party it was for:
// the Minecraft one names nothing but the user hash, the Xbox one carries the gamertag and id.
type authorized struct {
	Token    string
	UserHash string
	Gamertag string
	XUID     string
}

type xboxAnswer struct {
	Token         string `json:"Token"`
	NotAfter      string `json:"NotAfter"`
	DisplayClaims struct {
		XUI []struct {
			UserHash string `json:"uhs"`
			Gamertag string `json:"gtg"`
			XUID     string `json:"xid"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (s *SignIn) authorize(ctx context.Context, party, userToken string) (authorized, error) {
	body := map[string]any{
		"Properties":   map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{userToken}},
		"RelyingParty": party,
		"TokenType":    "JWT",
	}
	var res xboxAnswer
	if err := s.Client.PostJSON(ctx, s.XSTSURL+"/xsts/authorize", body, &res); err != nil {
		return authorized{}, xstsFailed(err)
	}
	a := authorized{Token: res.Token}
	if len(res.DisplayClaims.XUI) > 0 {
		claims := res.DisplayClaims.XUI[0]
		a.UserHash, a.Gamertag, a.XUID = claims.UserHash, claims.Gamertag, claims.XUID
	}
	return a, nil
}

func (s *SignIn) minecraftToken(ctx context.Context, userHash, xsts string) (token, expires string, err error) {
	body := map[string]string{"identityToken": "XBL3.0 x=" + userHash + ";" + xsts}
	var res struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := s.Client.PostJSON(ctx, s.ServicesURL+"/authentication/login_with_xbox", body, &res); err != nil {
		return "", "", signInFailed(err, "Minecraft services wouldn't take the Xbox sign-in")
	}
	return res.Token, s.now().Add(time.Duration(res.ExpiresIn) * time.Second).UTC().Format(time.RFC3339), nil
}

// profile reads the Java profile the session token belongs to. A 404 there is the answer, not a
// failure: it means the account owns no Java Edition.
func (s *SignIn) profile(ctx context.Context, token string) (Profile, bool, error) {
	client := *s.Client
	client.Header = http.Header{"Authorization": {"Bearer " + token}}
	var p Profile
	ok, err := client.GetJSONIfFound(ctx, s.ServicesURL+"/minecraft/profile", &p)
	if err != nil {
		return Profile{}, false, signInFailed(err, "Minecraft services wouldn't say who this account is")
	}
	return p, ok, nil
}

func (s *SignIn) token(ctx context.Context, form url.Values) (Tokens, error) {
	var res struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := s.Client.PostForm(ctx, s.OAuthURL+"/token", form, &res); err != nil {
		if oa := oauth(err); oa != nil {
			return Tokens{}, oa
		}
		return Tokens{}, err
	}
	return Tokens{Access: res.Access, Refresh: res.Refresh}, nil
}

// oauthError is the JSON body a Microsoft token endpoint refuses a request with. The status says
// only that it was refused; which of pending, declined and expired it is, is in here.
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *oauthError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	return e.Code
}

func oauth(err error) *oauthError {
	var status *fetch.StatusError
	if !errors.As(err, &status) || len(status.Body) == 0 {
		return nil
	}
	var oa oauthError
	if json.Unmarshal(status.Body, &oa) != nil || oa.Code == "" {
		return nil
	}
	return &oa
}

// xboxError is what an XSTS refusal carries: an XErr naming a Microsoft account with no Xbox side,
// which is an account shulker can't sign in at all, whatever it does next.
var xboxErrors = map[int64]string{
	2148916233: "this Microsoft account has no Xbox profile; sign in at minecraft.net once to make one",
	2148916235: "Xbox Live isn't available in this account's region",
	2148916238: "this account is a child account, and has to be added to a family to use Xbox Live",
}

func xstsFailed(err error) error {
	var status *fetch.StatusError
	if errors.As(err, &status) && len(status.Body) > 0 {
		var res struct {
			XErr int64 `json:"XErr"`
		}
		if json.Unmarshal(status.Body, &res) == nil {
			if message, ok := xboxErrors[res.XErr]; ok {
				return out.Errorf("sign-in-failed", "%s", message)
			}
		}
	}
	return signInFailed(err, "Xbox Live wouldn't authorize the sign-in")
}

// signInFailed is any step of the chain going wrong. A network failure is left as it is, so the
// caller can tell "Microsoft said no" from "Microsoft wasn't reachable".
func signInFailed(err error, what string) error {
	if fetch.IsNetwork(err) {
		return err
	}
	return out.Errorf("sign-in-failed", "%s (%v)", what, err)
}

// Error is what a state that stops a launch reads as, with the line that fixes it.
func (s State) Error(name string) error {
	switch s {
	case SignInExpired:
		e := out.Errorf("account-sign-in-expired", "%s's Microsoft sign-in has expired", name)
		e.Rows = []out.Detail{{Label: "Fix", Text: "shulker accounts login", IsCommand: true}}
		return e
	case NoProfile:
		e := out.Errorf("account-not-playable", "%s owns no Java profile, so it can't launch anything", name)
		e.Rows = []out.Detail{{Label: "Fix", Text: "buy Minecraft: Java Edition at minecraft.net"}}
		return e
	}
	return nil
}

// holds reports whether an RFC 3339 expiry is still ahead of when it has to be. An empty or
// unreadable one never holds: a token shulker can't date is one it renews.
func holds(when string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, when)
	return err == nil && t.After(now)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
