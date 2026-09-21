package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/account"
)

// writeAccountStore puts a store beside the harness's config.json, the way a later `accounts login`
// will.
func writeAccountStore(t *testing.T, h *harness, accounts ...account.Account) {
	t.Helper()
	if err := account.Save(account.Path(h.config), account.Store{Accounts: accounts}); err != nil {
		t.Fatal(err)
	}
}

func ownAccount(name, id string) account.Account {
	return account.Account{Type: account.Microsoft, Profile: &account.Profile{ID: id, Name: name}, RefreshToken: "r-" + id}
}

func offlineAccount(name, id string) account.Account {
	return account.Account{Type: account.Offline, Profile: &account.Profile{ID: id, Name: name}}
}

const (
	notchID     = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	steveID     = "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"
	dinnerbone  = "0e05d36c-9cbd-4b0a-ae4e-7b2e2b7eb1f4"
	gamertagXID = "2533274812345678"
)

func TestAccountsEmpty(t *testing.T) {
	h := newHarness(t)
	if stdout := h.mustRun(t, "accounts"); !strings.Contains(stdout, "no accounts yet") {
		t.Errorf("empty state = %q", stdout)
	}
}

func TestAccountsListsEachGroupWithItsState(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h,
		ownAccount("Notch", notchID),
		account.Account{Type: account.Microsoft, Profile: &account.Profile{ID: dinnerbone, Name: "Dinnerbone"}},
		account.Account{Type: account.Microsoft, Xbox: &account.Xbox{XUID: gamertagXID, Gamertag: "Big Dog 42"}, RefreshToken: "r"},
		offlineAccount("Steve", steveID),
	)
	h.mustRun(t, "config", "set", "accounts.default", notchID)

	// Names and ids each align within their group, the way a list block's version column does.
	pad := strings.Repeat(" ", len(notchID)-len(gamertagXID))
	stdout := h.mustRun(t, "accounts")
	for _, want := range []string{
		"  Own\n",
		"  ✔ Notch      " + notchID + " playable",
		"  • Big Dog 42 " + gamertagXID + pad + " not playable (no Java profile)",
		"  • Dinnerbone " + dinnerbone + " sign-in expired",
		"  Offline\n",
		"  • Steve " + steveID + " offline",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("accounts is missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Borrowed") {
		t.Errorf("no provider yields borrowed accounts yet:\n%s", stdout)
	}
}

func TestAccountsJSON(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, ownAccount("Notch", notchID), offlineAccount("Steve", steveID))
	h.mustRun(t, "config", "set", "accounts.default", steveID)

	var rows []accountRow
	if err := json.Unmarshal(h.runSetting(t, 0, "accounts", "--json").Data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Group != account.GroupOwn || rows[0].State != account.Playable || rows[0].Default {
		t.Errorf("own row = %+v", rows[0])
	}
	if rows[1].Group != account.GroupOffline || rows[1].Source != "offline" || !rows[1].Default {
		t.Errorf("offline row = %+v", rows[1])
	}
}

func TestAccountsStoreIsPrivateAndCarriesItsSchema(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, ownAccount("Notch", notchID))
	path := account.Path(h.config)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("accounts.json mode = %o, want 600", mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"$schema": "https://shulker.sh/schema/v1/accounts.json"`) {
		t.Errorf("accounts.json has no $schema line:\n%s", data)
	}
}

func TestAccountsInvalidFile(t *testing.T) {
	for _, c := range []struct{ name, data, code string }{
		{"no marker", `{"accounts":[]}`, "accounts-invalid"},
		{"newer shulker", `{"$schema":"https://shulker.sh/schema/v9/accounts.json"}`, "schema-newer"},
		{"broken json", `{`, "accounts-invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			if err := os.WriteFile(account.Path(h.config), []byte(c.data), 0o600); err != nil {
				t.Fatal(err)
			}
			code, stdout, _ := h.run(t, "accounts", "--json")
			if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
				t.Fatalf("exit %d, want %s: %s", code, c.code, stdout)
			}
		})
	}
}

func TestAccountsUse(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, ownAccount("Notch", notchID), offlineAccount("Steve", steveID))

	if stdout := h.mustRun(t, "accounts", "use", "Steve"); !strings.Contains(stdout, "Steve is now the default account") {
		t.Errorf("use output = %q", stdout)
	}
	doc := readConfigDoc(t, h.config)
	accounts, _ := doc["accounts"].(map[string]any)
	if accounts["default"] != steveID {
		t.Fatalf("accounts.default = %v, want %s", accounts["default"], steveID)
	}
	if stdout := h.mustRun(t, "accounts"); !strings.Contains(stdout, "✔ Steve") {
		t.Errorf("the default should be marked:\n%s", stdout)
	}
	// A UUID selects too, undashed and in any case.
	h.mustRun(t, "accounts", "use", strings.ToUpper(strings.ReplaceAll(notchID, "-", "")))
	if readConfigDoc(t, h.config)["accounts"].(map[string]any)["default"] != notchID {
		t.Errorf("an undashed uuid should select: %v", readConfigDoc(t, h.config))
	}
}

func TestAccountsUseRefusesAnAccountThatCannotLaunch(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, account.Account{
		Type: account.Microsoft,
		Xbox: &account.Xbox{XUID: gamertagXID, Gamertag: "Big Dog 42"}, RefreshToken: "r",
	})
	code, stdout, _ := h.run(t, "accounts", "use", "Big Dog 42", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "account-not-playable" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestAccountsSelectorFailures(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "accounts", "use", "Notch", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-accounts" {
		t.Fatalf("with no accounts: %s", stdout)
	}

	writeAccountStore(t, h, ownAccount("Notch", notchID), offlineAccount("Notch", steveID))
	code, stdout, _ = h.run(t, "accounts", "use", "Dinnerbone", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "account-not-found" {
		t.Fatalf("no match: %s", stdout)
	}

	// Two accounts share the name, and shulker can't ask: each match is named by its qualifier and
	// its id, which is what tells them apart.
	code, stdout, _ = h.run(t, "accounts", "use", "Notch", "--json")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "ambiguous-account" || len(e.Candidates) != 2 {
		t.Fatalf("ambiguous: %s", stdout)
	}
	if !strings.Contains(e.Candidates[0], "Notch@shulker") || !strings.Contains(e.Candidates[0], notchID) {
		t.Errorf("candidate = %q", e.Candidates[0])
	}
	if !strings.Contains(e.Candidates[1], "Notch@offline") {
		t.Errorf("candidate = %q", e.Candidates[1])
	}

	// The qualifier resolves it.
	h.mustRun(t, "accounts", "use", "Notch@offline")
	if readConfigDoc(t, h.config)["accounts"].(map[string]any)["default"] != steveID {
		t.Error("Notch@offline should have won")
	}
}

func TestAccountsProviders(t *testing.T) {
	h := newHarness(t)
	writeAccountStore(t, h, ownAccount("Notch", notchID))

	if stdout := h.mustRun(t, "config", "get", "accounts.providers"); stdout != "[\n  \"shulker\"\n]\n" {
		t.Errorf("the default should read back: %q", stdout)
	}
	if stdout := h.mustRun(t, "accounts"); !strings.Contains(stdout, "Notch") {
		t.Fatalf("the default list reads shulker's own accounts:\n%s", stdout)
	}

	// shulker can be taken out of the list, and then its own accounts stop being visible.
	registerMojang(t, h, t.TempDir())
	h.mustRun(t, "config", "set", "accounts.providers", "--literal", `["mojang"]`)
	stdout, stderr := h.mustRunStderr(t, "accounts")
	if !strings.Contains(stdout, "no accounts yet") {
		t.Errorf("without shulker its own accounts are not read:\n%s", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("a launcher with nothing to read says nothing: %q", stderr)
	}

	// Unsetting the key goes back to the default, and the accounts come back.
	h.mustRun(t, "config", "unset", "accounts.providers")
	if stdout := h.mustRun(t, "accounts"); !strings.Contains(stdout, "Notch") {
		t.Errorf("unset should restore the default:\n%s", stdout)
	}
}

func TestConfigSetLiteral(t *testing.T) {
	h := newHarness(t)

	for _, c := range []struct{ name, value, code string }{
		{"not a list", `"shulker"`, "usage"},
		{"empty", `[]`, "usage"},
		{"repeated", `["shulker","shulker"]`, "usage"},
		{"unknown launcher", `["technic"]`, "usage"},
		{"not a name", `[1]`, "usage"},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, _ := h.run(t, "config", "set", "accounts.providers", "--literal", c.value, "--json")
			if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
				t.Fatalf("exit %d: %s", code, stdout)
			}
		})
	}

	// Without --literal the value would be the string "[]", which the key can't hold.
	code, stdout, _ := h.run(t, "config", "set", "accounts.providers", `["shulker"]`, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
		t.Fatalf("a list needs --literal: %s", stdout)
	}

	h.mustRun(t, "config", "set", "accounts.providers", "--literal", `["shulker"]`)
	doc := readConfigDoc(t, h.config)
	got, _ := doc["accounts"].(map[string]any)["providers"].([]any)
	if len(got) != 1 || got[0] != "shulker" {
		t.Fatalf("accounts.providers = %v", doc["accounts"])
	}
	if data, err := os.ReadFile(filepath.Join(h.config)); err != nil || !strings.Contains(string(data), `"providers"`) {
		t.Fatalf("config.json: %v %s", err, data)
	}
}
