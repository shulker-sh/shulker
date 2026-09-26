package play

import (
	"context"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

func TestAccountTakesTheSelectorOverTheDefault(t *testing.T) {
	h := newHarness(t, "")
	h.accounts(ownAccount("Notch", notchID), ownAccount("Jeb_", jebID))
	h.setConfig("accounts", "default", notchID)
	plan := h.mustAssemble(Request{})

	who, adopt, err := Account(h.e, plan, "Jeb_")
	if err != nil || who.Name != "Jeb_" || adopt {
		t.Fatalf("the selector names who plays and changes nothing: %+v adopt=%v err=%v", who, adopt, err)
	}
	who, adopt, err = Account(h.e, plan, "")
	if err != nil || who.Name != "Notch" || adopt {
		t.Fatalf("without a selector the default plays: %+v adopt=%v err=%v", who, adopt, err)
	}
}

func TestAccountHonoursThePinAndRefusesOneThatIsGone(t *testing.T) {
	h := newHarness(t, "")
	h.accounts(ownAccount("Notch", notchID), offlineAccount("Steve", steveID))
	h.setConfig("accounts", "default", notchID)
	plan := h.mustAssemble(Request{})

	plan.Settings.Account = steveID
	who, adopt, err := Account(h.e, plan, "")
	if err != nil || who.Name != "Steve" || adopt {
		t.Fatalf("the pin beats the default: %+v adopt=%v err=%v", who, adopt, err)
	}
	if who, _, err := Account(h.e, plan, "Notch"); err != nil || who.Name != "Notch" {
		t.Fatalf("the selector beats the pin: %+v err=%v", who, err)
	}

	plan.Settings.Account = jebID
	_, _, err = Account(h.e, plan, "")
	if out.CodeOf(err) != "account-not-found" || !strings.Contains(out.AsError(err).Nudge.Command, "instance unset account") {
		t.Fatalf("a pin whose account is gone fails the launch rather than playing as someone else: %v", err)
	}
}

func TestAccountWithNoDefaultTakesTheOnlyAccountThereIs(t *testing.T) {
	h := newHarness(t, "")
	h.accounts(ownAccount("Notch", notchID))
	plan := h.mustAssemble(Request{})

	who, adopt, err := Account(h.e, plan, "")
	if err != nil || who.Name != "Notch" || !adopt {
		t.Fatalf("the only account plays and becomes the default: %+v adopt=%v err=%v", who, adopt, err)
	}
}

func TestAccountWithNoDefaultAndSeveralAccountsAsksOrNamesTheFlag(t *testing.T) {
	h := newHarness(t, "")
	h.accounts(ownAccount("Notch", notchID), offlineAccount("Steve", steveID))
	plan := h.mustAssemble(Request{})

	_, _, err := Account(h.e, plan, "")
	if e := out.AsError(err); out.CodeOf(err) != "usage" || e.Flag != "--account" || len(e.Candidates) != 2 {
		t.Fatalf("off a terminal the launch names the flag and the choices: %v", err)
	}

	h.e.AskAccount = func(matches []account.Resolved) (account.Resolved, bool, error) {
		return matches[1], true, nil
	}
	who, adopt, err := Account(h.e, plan, "")
	if err != nil || who.Name != "Steve" || !adopt {
		t.Fatalf("the picked account plays and becomes the default: %+v adopt=%v err=%v", who, adopt, err)
	}

	h.e.AskAccount = func([]account.Resolved) (account.Resolved, bool, error) { return account.Resolved{}, false, nil }
	if _, _, err := Account(h.e, plan, ""); out.CodeOf(err) != "usage" {
		t.Fatalf("an escaped picker lands where no picker does: %v", err)
	}
}

func TestAccountWithNoAccountAtAllSaysThereIsNothingToPlayWith(t *testing.T) {
	h := newHarness(t, "")
	plan := h.mustAssemble(Request{})

	if _, _, err := Account(h.e, plan, ""); out.CodeOf(err) != "no-accounts" {
		t.Fatalf("err %v", err)
	}
}

func TestAccountRefusesAnAccountWhoseSignInHasExpired(t *testing.T) {
	h := newHarness(t, "")
	h.accounts(account.Account{Type: account.Microsoft, Profile: &account.Profile{ID: jebID, Name: "Dinnerbone"}})
	plan := h.mustAssemble(Request{})

	_, _, err := Account(h.e, plan, "")
	if err == nil || !strings.Contains(err.Error(), "Dinnerbone's Microsoft sign-in has expired") {
		t.Fatalf("err %v", err)
	}
	if e := out.AsError(err); len(e.Rows) == 0 || !strings.Contains(e.Rows[0].Text, "shulker accounts login") {
		t.Fatalf("the refusal has to carry its fix line: %+v", e)
	}
}

func TestSessionPlaysOnWhatItHasAndWarnsWhenOnlineWillReject(t *testing.T) {
	h := newHarness(t, "")

	steve := offlineAccount("Steve", steveID).Resolved()
	signed, err := Session(context.Background(), h.e, steve)
	if err != nil || signed.ID() != steveID || len(h.env.Warnings) != 0 {
		t.Fatalf("an offline account plays as it is: %+v err=%v warnings=%v", signed, err, h.env.Warnings)
	}

	expired := account.Resolved{ID: notchID, Name: "Notch", Source: "prism", Group: account.GroupLauncher, State: account.TokenExpired, Account: ownAccount("Notch", notchID)}
	if _, err := Session(context.Background(), h.e, expired); err != nil || !h.warned("Notch's session token has run out and only Prism Launcher can renew it") {
		t.Fatalf("a launcher's expired token plays with a warning: err=%v warnings=%v", err, h.env.Warnings)
	}
}
