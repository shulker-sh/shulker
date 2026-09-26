package cli

import (
	"strings"
	"testing"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

// steveOffline is the UUID an offline-mode host derives for "Steve", which is what `accounts add`
// gives an account it is not told one for.
const steveOffline = "5627dd98-e6be-3c21-b8a8-e92344183641"

// withOwner is a harness that can create offline accounts: the gate wants an account with a Java
// profile in sight, and signing Notch in is how a player proves it.
func withOwner(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	writeAccountStore(t, h, ownAccount("Notch", notchID))
	return h
}

func TestAccountsAddCreatesAnOfflineAccount(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "config", "set", "accounts.default", notchID)

	stdout := h.mustRun(t, "accounts", "add", "Steve")
	if !strings.Contains(stdout, "✔ Created the offline account Steve ("+steveOffline+")") {
		t.Errorf("add result: %s", stdout)
	}
	if !strings.Contains(stdout, "shulker accounts use Steve") {
		t.Errorf("without --use it prints the line that switches: %s", stdout)
	}

	store := readAccountStore(t, h)
	if len(store.Accounts) != 2 {
		t.Fatalf("accounts = %+v", store.Accounts)
	}
	created := store.Accounts[1]
	if created.Type != account.Offline || created.Profile.Name != "Steve" || created.Profile.ID != steveOffline {
		t.Errorf("created = %+v", created)
	}
	if created.RefreshToken != "" || created.Minecraft != nil {
		t.Errorf("an offline account holds no token: %+v", created)
	}
	if !strings.Contains(h.mustRun(t, "accounts"), "Steve    "+steveOffline+"  offline  offline") {
		t.Error("the account should list under Offline")
	}
	if readConfigDoc(t, h.config)["accounts"].(map[string]any)["default"] != notchID {
		t.Error("add left the default account alone")
	}
}

func TestAccountsAddUseSwitchesTheDefault(t *testing.T) {
	h := withOwner(t)
	stdout := h.mustRun(t, "accounts", "add", "Steve", "--use")
	if !strings.Contains(stdout, "now the default account") {
		t.Errorf("--use result: %s", stdout)
	}
	if !strings.Contains(h.mustRun(t, "accounts"), "✔  Steve") {
		t.Error("the account it created should be marked the default")
	}
}

func TestAccountsAddUUIDPinsAnotherAndNeverWarns(t *testing.T) {
	h := withOwner(t)
	stdout, stderr := h.mustRunStderr(t, "accounts", "add", "Steve", "--uuid", strings.ToUpper(strings.ReplaceAll(steveID, "-", "")))
	if !strings.Contains(stdout, steveID) {
		t.Errorf("a uuid reads undashed and in any case: %s", stdout)
	}
	if strings.Contains(stderr, "warning") {
		t.Errorf("a pinned uuid differs from the derived one on purpose, so nothing is said: %s", stderr)
	}
	if got := readAccountStore(t, h).Accounts[1].Profile.ID; got != steveID {
		t.Errorf("stored id = %s, want %s", got, steveID)
	}
}

func TestAccountsAddRejectsAUUIDThatIsNotOne(t *testing.T) {
	h := withOwner(t)
	code, stdout, _ := h.run(t, "accounts", "add", "Steve", "--uuid", "not-a-uuid", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestAccountsAddChecksTheNameAgainstThePlayerPattern(t *testing.T) {
	h := withOwner(t)
	code, stdout, _ := h.run(t, "accounts", "add", "Steve the Wanderer", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "account-name-invalid" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	h.mustRun(t, "accounts", "add", "Steve the Wanderer", "--allow-invalid-name")
	if got := readAccountStore(t, h).Accounts[1].Profile.Name; got != "Steve the Wanderer" {
		t.Errorf("stored name = %q", got)
	}
}

func TestAccountsAddRefusesADuplicate(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve")

	// The same name derives the same UUID, so a second Steve is the same account: refused however
	// hard the run insists.
	for _, args := range [][]string{
		{"accounts", "add", "Steve", "--json"},
		{"accounts", "add", "Steve", "--force", "--json"},
		{"accounts", "add", "Steve", "--force", "--uuid", steveOffline, "--json"},
	} {
		code, stdout, _ := h.run(t, args...)
		if e := failureCode(t, stdout); code == 0 || e.Code != "account-exists" {
			t.Fatalf("%v: exit %d: %s", args, code, stdout)
		}
	}

	// A name clash on its own is allowed, once the run says so.
	h.mustRun(t, "accounts", "add", "Steve", "--force", "--uuid", steveID)
	if store := readAccountStore(t, h); len(store.Accounts) != 3 {
		t.Fatalf("accounts = %+v", store.Accounts)
	}

	// A clash with a signed-in account says which kind of account is in the way.
	code, stdout, _ := h.run(t, "accounts", "add", "Notch", "--json")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "account-exists" || !strings.Contains(e.Message, "signed in") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestAccountsAddRefusesAUUIDThatIsAlreadyAnAccount(t *testing.T) {
	h := withOwner(t)
	code, stdout, _ := h.run(t, "accounts", "add", "Steve", "--force", "--uuid", notchID, "--json")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "account-exists" || !strings.Contains(e.Message, "Notch") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestAccountsAddNeedsAnAccountThatOwnsTheGame(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "accounts", "add", "Steve", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ownership-unproven" {
		t.Fatalf("with no account at all: exit %d: %s", code, stdout)
	}

	// An account that owns no Java profile proves nothing, and neither does another offline one.
	writeAccountStore(t, h,
		account.Account{Type: account.Microsoft, Xbox: &account.Xbox{XUID: gamertagXID, Gamertag: "Big Dog 42"}, RefreshToken: "r"},
		offlineAccount("Alex", steveID),
	)
	code, stdout, _ = h.run(t, "accounts", "add", "Steve", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ownership-unproven" {
		t.Fatalf("with nothing playable: exit %d: %s", code, stdout)
	}
}

func TestAccountsAddGateTakesAnyAccountWithAJavaProfile(t *testing.T) {
	signedOut := newHarness(t)
	expired := ownAccount("Notch", notchID)
	expired.RefreshToken = ""
	writeAccountStore(t, signedOut, expired)
	signedOut.mustRun(t, "accounts", "add", "Steve")

	fromPrism := newHarness(t)
	prismAccounts(t, fromPrism, `{"formatVersion": 3, "accounts": [{"type": "MSA", "ygg": {"token": "stale", "exp": 1600000000},
	  "profile": {"id": "853c80ef-3c37-49fd-aa49-938b674adae6", "name": "Jeb_"}}]}`)
	fromPrism.mustRun(t, "accounts", "stores", "add", "prism")
	fromPrism.mustRun(t, "accounts", "add", "Steve")
}

func TestAccountsAddGateIsCheckedOnlyAtCreation(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve", "--use")
	h.mustRun(t, "accounts", "logout", "Notch", "--yes")

	if stdout := h.mustRun(t, "accounts"); !strings.Contains(stdout, "✔  Steve") {
		t.Errorf("the offline account outlives the one that proved ownership:\n%s", stdout)
	}
	h.mustRun(t, "accounts", "use", "Steve")
}

func TestAccountsRemoveDeletesAnOfflineAccount(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve")

	// Confirming is the default, and a run that can't ask needs the flag that answers it.
	code, stdout, _ := h.run(t, "accounts", "remove", "Steve", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Help, "--yes") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if len(readAccountStore(t, h).Accounts) != 2 {
		t.Fatal("nothing should be removed without an answer")
	}

	stdout = h.mustRun(t, "accounts", "remove", "Steve", "--yes")
	if !strings.Contains(stdout, "✔ Removed Steve ("+steveOffline+")") {
		t.Errorf("remove result: %s", stdout)
	}
	if store := readAccountStore(t, h); len(store.Accounts) != 1 || store.Accounts[0].Type != account.Microsoft {
		t.Errorf("accounts = %+v", store.Accounts)
	}
}

func TestAccountsRemoveNamesLogoutForAMicrosoftAccount(t *testing.T) {
	h := withOwner(t)
	code, stdout, _ := h.run(t, "accounts", "remove", "Notch", "--yes", "--json")
	e := failureCode(t, stdout)
	if code != out.ExitUsage || e.Code != "usage" || !strings.Contains(e.Help, "accounts logout Notch") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if len(readAccountStore(t, h).Accounts) != 1 {
		t.Error("the account should still be there")
	}
}

func TestAccountsRemoveNeedsTheOwnerThatCouldRecreateIt(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve", "--use")
	h.mustRun(t, "accounts", "logout", "Notch", "--yes")

	code, stdout, _ := h.run(t, "accounts", "remove", "Steve", "--yes", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ownership-unproven" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if len(readAccountStore(t, h).Accounts) != 1 {
		t.Fatal("the refusal should leave the account alone")
	}

	stdout = h.mustRun(t, "accounts", "remove", "Steve", "--yes", "--force")
	if store := readAccountStore(t, h); len(store.Accounts) != 0 {
		t.Errorf("--force should remove it anyway: %+v", store.Accounts)
	}
	// Nothing is left to take the default over.
	if !strings.Contains(stdout, "No default account now") {
		t.Errorf("remove result: %s", stdout)
	}
	if cfg, ok := readConfigDoc(t, h.config)["accounts"].(map[string]any); ok && cfg["default"] != nil {
		t.Error("accounts.default still names an account that is gone")
	}
}

func TestAccountsRemoveReseatsTheDefault(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve", "--use")

	// An offline account launches as readily as a signed-in one, so the one account left behind
	// takes the default whichever kind it is.
	stdout := h.mustRun(t, "accounts", "remove", "Steve", "--yes")
	if !strings.Contains(stdout, "Notch is the default account now") {
		t.Fatalf("remove result: %s", stdout)
	}
	if readConfigDoc(t, h.config)["accounts"].(map[string]any)["default"] != notchID {
		t.Error("the playable account left should be the default")
	}
}

func TestAccountsLogoutReseatsOntoAnOfflineAccount(t *testing.T) {
	h := withOwner(t)
	h.mustRun(t, "accounts", "add", "Steve")
	h.mustRun(t, "accounts", "use", "Notch")

	stdout := h.mustRun(t, "accounts", "logout", "Notch", "--yes")
	if !strings.Contains(stdout, "Steve is the default account now") {
		t.Fatalf("logout result: %s", stdout)
	}
	if readConfigDoc(t, h.config)["accounts"].(map[string]any)["default"] != steveOffline {
		t.Error("the offline account left should be the default")
	}
}
