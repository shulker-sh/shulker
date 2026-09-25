package account

import (
	"context"
	"testing"
	"time"

	"shulker.sh/shulker/internal/out"
)

func ownAccount(a Account) Resolved {
	return Resolved{ID: a.ID(), Name: a.Name(), Source: SourceShulker, Group: GroupOwn, State: a.State(), Account: a}
}

func TestSessionReusesAFreshTokenAndRenewsAStaleOne(t *testing.T) {
	f := newFakeMSA(t)
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	s := f.signIn()
	grants := len(f.grants)
	a.Minecraft.ExpiresAt = signInNow.Add(24 * time.Hour).Format(time.RFC3339)
	if _, renewed, warning, err := s.Session(context.Background(), ownAccount(a), signInNow); err != nil || renewed || warning != WarnNone {
		t.Fatalf("a token with a day left should be reused: renewed %v, warning %q, err %v", renewed, warning, err)
	}
	if len(f.grants) != grants {
		t.Errorf("a fresh token asked Microsoft again: grants %q", f.grants)
	}

	// Inside the hour it is renewed before the game starts rather than during it.
	a.Minecraft.ExpiresAt = signInNow.Add(30 * time.Minute).Format(time.RFC3339)
	a.Xbox.NotAfter = signInNow.Add(-time.Hour).Format(time.RFC3339)
	f.refresh = "refresh-2"
	signed, renewed, warning, err := s.Session(context.Background(), ownAccount(a), signInNow)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed || warning != WarnNone || signed.RefreshToken != "refresh-2" {
		t.Errorf("renewed = %v, warning = %q, refresh token = %q", renewed, warning, signed.RefreshToken)
	}
}

func TestSessionOfflineKeepsTheCachedToken(t *testing.T) {
	f := newFakeMSA(t)
	a, err := f.login(t)
	if err != nil {
		t.Fatal(err)
	}
	a.Minecraft.ExpiresAt = signInNow.Add(time.Minute).Format(time.RFC3339)
	a.Xbox.NotAfter = signInNow.Add(-time.Hour).Format(time.RFC3339)
	s := f.signIn()
	s.Client.Offline = true
	signed, renewed, warning, err := s.Session(context.Background(), ownAccount(a), signInNow)
	if err != nil {
		t.Fatalf("offline, a launch goes ahead on what it has: %v", err)
	}
	if renewed || signed.Minecraft.Token != "minecraft-token" {
		t.Errorf("renewed = %v, session token = %q", renewed, signed.Minecraft.Token)
	}
	if warning != WarnOffline {
		t.Errorf("an offline launch has to say what won't work: warning = %q", warning)
	}
}

func TestSessionRefusesAnExpiredOrProfilelessAccount(t *testing.T) {
	s := newFakeMSA(t).signIn()
	expired := ownAccount(Account{Type: Microsoft, Profile: &Profile{ID: "0e05d36c-9cbd-4b0a-ae4e-7b2e2b7eb1f4", Name: "Dinnerbone"}})
	if _, _, _, err := s.Session(context.Background(), expired, signInNow); out.CodeOf(err) != "account-sign-in-expired" {
		t.Errorf("err = %v (%s)", err, out.CodeOf(err))
	} else if rows := out.AsError(err).Rows; len(rows) != 1 || rows[0].Text != "shulker accounts login" {
		t.Errorf("rows = %+v", rows)
	}
	profileless := ownAccount(Account{Type: Microsoft, Xbox: &Xbox{XUID: "2533274812345678", Gamertag: "Big Dog 42"}, RefreshToken: "r"})
	if _, _, _, err := s.Session(context.Background(), profileless, signInNow); out.CodeOf(err) != "account-not-playable" {
		t.Errorf("err = %v (%s)", err, out.CodeOf(err))
	}
}

func TestSessionWarnsOnALauncherTokenThatRanOut(t *testing.T) {
	s := newFakeMSA(t).signIn()
	a := Account{Type: Microsoft, Profile: &Profile{ID: "id", Name: "Jeb_"}, Minecraft: &Minecraft{Token: "stale", ExpiresAt: signInNow.Add(-time.Hour).Format(time.RFC3339)}}
	r := Resolved{ID: "id", Name: "Jeb_", Source: "prism", Group: GroupLauncher, State: TokenExpired, Account: a}
	signed, renewed, warning, err := s.Session(context.Background(), r, signInNow)
	if err != nil {
		t.Fatalf("an expired launcher account still launches: %v", err)
	}
	if renewed || warning != WarnTokenExpired || signed.Minecraft.Token != "stale" {
		t.Errorf("renewed = %v, warning = %q, token = %q", renewed, warning, signed.Minecraft.Token)
	}
}
