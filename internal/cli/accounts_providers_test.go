package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
)

// registerPrism puts a prism instance in the registry so the reader looks in dir, the way
// `link prism --launcher-dir` leaves it.
func registerPrism(t *testing.T, h *harness, dir string) {
	t.Helper()
	if _, err := config.UpdateInstances(registryPath(h), func(instances []config.Instance) []config.Instance {
		return append(instances, config.Instance{
			ID: "borrowed", Launcher: "prism", LauncherDir: dir,
			Dir: filepath.Join(dir, "instances", "borrowed", "minecraft"), Source: ".",
		})
	}); err != nil {
		t.Fatal(err)
	}
}

// prismAccounts writes a Prism account list into a fresh data directory and registers it.
func prismAccounts(t *testing.T, h *harness, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, account.PrismFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	registerPrism(t, h, dir)
	return dir
}

const prismNotch = `{
  "formatVersion": 3,
  "accounts": [
    {"type": "MSA", "ygg": {"token": "session", "exp": 4102444800},
     "profile": {"id": "069a79f4-44e9-4726-a5be-fca90e38aaf5", "name": "Notch"}},
    {"type": "MSA", "ygg": {"token": "stale", "exp": 1600000000},
     "profile": {"id": "853c80ef-3c37-49fd-aa49-938b674adae6", "name": "Jeb_"}},
    {"type": "Offline", "ygg": {"token": "0"},
     "profile": {"id": "5627dd98-e6be-3c21-b8a8-e92344183641", "name": "Steve"}}
  ]
}`

func TestProvidersList(t *testing.T) {
	h := newHarness(t)
	if stdout := h.mustRun(t, "accounts", "providers"); !strings.Contains(stdout, "• shulker Shulker\n") {
		t.Errorf("the default list = %q", stdout)
	}
	h.mustRun(t, "accounts", "providers", "set", "prism", "mojang", "shulker")

	stdout := h.mustRun(t, "accounts", "providers")
	for _, want := range []string{
		"• prism   Prism Launcher\n",
		"• mojang  Minecraft Launcher no reader yet",
		"• shulker Shulker\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("providers is missing %q:\n%s", want, stdout)
		}
	}
	var names []string
	if err := json.Unmarshal(h.runSetting(t, 0, "accounts", "providers", "--json").Data, &names); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "prism,mojang,shulker" {
		t.Errorf("json = %v, want the list in order", names)
	}
}

func TestProvidersAddAndRemovePrintTheListBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	// A directory the launcher really is in, so opting in says nothing about it.
	prismAccounts(t, h, prismNotch)

	stdout := h.mustRun(t, "accounts", "providers", "add", "prism")
	if !strings.Contains(stdout, `accounts.providers ["shulker"] ⟶ ["shulker","prism"]`) {
		t.Errorf("add should print the list before and after:\n%s", stdout)
	}
	if got := readConfigDoc(t, h.config)["accounts"].(map[string]any)["providers"]; len(got.([]any)) != 2 {
		t.Errorf("accounts.providers = %v", got)
	}
	stdout = h.mustRun(t, "accounts", "providers", "remove", "shulker")
	if !strings.Contains(stdout, `accounts.providers ["shulker","prism"] ⟶ ["prism"]`) {
		t.Errorf("remove should print the list before and after:\n%s", stdout)
	}
	stdout = h.mustRun(t, "accounts", "providers", "set", "shulker", "mojang")
	if !strings.Contains(stdout, `accounts.providers ["prism"] ⟶ ["shulker","mojang"]`) {
		t.Errorf("set should print the list before and after:\n%s", stdout)
	}
}

func TestProvidersRejectWhatTheListCannotHold(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		name, want string
		args       []string
	}{
		{"unknown launcher", "can't read accounts from technic", []string{"add", "technic"}},
		{"unknown in a set", "can't read accounts from technic", []string{"set", "shulker", "technic"}},
		{"already in the list", "already reads accounts from shulker", []string{"add", "shulker"}},
		{"repeated in a set", "names prism twice", []string{"set", "prism", "prism"}},
		{"not in the list", "does not read accounts from prism", []string{"remove", "prism"}},
		{"emptied", "can't be empty", []string{"remove", "shulker"}},
		{"set to nothing", "can't be empty", []string{"set"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, _ := h.run(t, append([]string{"accounts", "providers"}, append(c.args, "--json")...)...)
			e := failureCode(t, stdout)
			if code == 0 || e.Code != "usage" {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			if !strings.Contains(e.Message, c.want) {
				t.Errorf("message = %q, want %q in it", e.Message, c.want)
			}
		})
	}
	if _, err := os.Stat(h.config); !os.IsNotExist(err) {
		t.Errorf("a refused change must not be written: %v", err)
	}
}

func TestProvidersWarnOnceAboutALauncherThatIsNotThere(t *testing.T) {
	h := newHarness(t)
	missing := filepath.Join(t.TempDir(), "gone")
	registerPrism(t, h, missing)

	_, stderr := h.mustRunStderr(t, "accounts", "providers", "add", "prism")
	if !strings.Contains(stderr, "Prism Launcher isn't at "+missing) {
		t.Fatalf("add should name the directory it checked:\n%s", stderr)
	}
	// Every later run stays quiet: the launcher may yet be installed, and this is not an error.
	for _, args := range [][]string{{"accounts"}, {"accounts", "providers"}, {"accounts", "providers", "set", "prism"}} {
		if _, stderr := h.mustRunStderr(t, args...); strings.Contains(stderr, missing) {
			t.Errorf("%v should not warn about the directory:\n%s", args, stderr)
		}
	}
	// `set` opts in the same way `add` does, so a launcher it brings in warns once too.
	h.mustRun(t, "accounts", "providers", "set", "shulker")
	_, stderr = h.mustRunStderr(t, "accounts", "providers", "set", "shulker", "prism")
	if !strings.Contains(stderr, "Prism Launcher isn't at "+missing) {
		t.Fatalf("set should name the directory it checked:\n%s", stderr)
	}
}

func TestAccountsBorrowsFromPrism(t *testing.T) {
	h := newHarness(t)
	prismAccounts(t, h, prismNotch)
	writeAccountStore(t, h, ownAccount("Dinnerbone", dinnerbone))
	h.mustRun(t, "accounts", "providers", "add", "prism")

	stdout := h.mustRun(t, "accounts")
	for _, want := range []string{
		"  Own\n",
		"• Dinnerbone " + dinnerbone + " playable",
		"  Borrowed\n",
		"• Jeb_  853c80ef-3c37-49fd-aa49-938b674adae6 token expired ",
		"• Notch 069a79f4-44e9-4726-a5be-fca90e38aaf5 playable",
		"• Steve 5627dd98-e6be-3c21-b8a8-e92344183641 offline",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("accounts is missing %q:\n%s", want, stdout)
		}
	}
	var rows []accountRow
	if err := json.Unmarshal(h.runSetting(t, 0, "accounts", "--json").Data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows[1:] {
		if row.Source != "prism" || row.Group != account.GroupBorrowed {
			t.Errorf("borrowed row = %+v", row)
		}
	}
	// A borrowed account is selected by @prism and named by the launcher it belongs to.
	h.mustRun(t, "accounts", "use", "Notch@prism")
	code, stdout, _ := h.run(t, "accounts", "logout", "Notch", "--yes", "--json")
	if e := failureCode(t, stdout); code == 0 || !strings.Contains(e.Message, "borrowed from prism") {
		t.Fatalf("logout on a borrowed account: %s", stdout)
	}
}

func TestAccountsDedupeKeepsTheEarliestProvider(t *testing.T) {
	h := newHarness(t)
	prismAccounts(t, h, prismNotch)
	writeAccountStore(t, h, ownAccount("Notch", notchID))

	h.mustRun(t, "accounts", "providers", "set", "shulker", "prism")
	stdout := h.mustRun(t, "accounts")
	if !strings.Contains(stdout, "Own") || strings.Count(stdout, notchID) != 1 {
		t.Errorf("shulker comes first, so its own Notch is the only one:\n%s", stdout)
	}
	h.mustRun(t, "accounts", "providers", "set", "prism", "shulker")
	stdout = h.mustRun(t, "accounts")
	if strings.Contains(stdout, "  Own\n") || strings.Count(stdout, notchID) != 1 {
		t.Errorf("prism comes first, so its Notch is the only one:\n%s", stdout)
	}
}

func TestAccountsSkipsAPrismFileItCannotRead(t *testing.T) {
	h := newHarness(t)
	dir := prismAccounts(t, h, `{"formatVersion":3,`)
	writeAccountStore(t, h, ownAccount("Dinnerbone", dinnerbone))
	h.mustRun(t, "accounts", "providers", "add", "prism")

	stdout, stderr := h.mustRunStderr(t, "accounts")
	if !strings.Contains(stderr, filepath.Join(dir, account.PrismFileName)) {
		t.Errorf("the warning should name the file:\n%s", stderr)
	}
	if !strings.Contains(stdout, "Dinnerbone") {
		t.Errorf("a file shulker can't read must not take the list down:\n%s", stdout)
	}
}

func TestAccountsSaysNothingAboutAnEmptyOrAbsentPrismFile(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"signed out", `{"formatVersion":3,"accounts":[]}`},
		{"no profile on the entry", `{"formatVersion":3,"accounts":[{"type":"MSA","username":"someone@example.com"}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			prismAccounts(t, h, c.body)
			h.mustRun(t, "accounts", "providers", "set", "prism")
			stdout, stderr := h.mustRunStderr(t, "accounts")
			if !strings.Contains(stdout, "no accounts yet") {
				t.Errorf("stdout = %q", stdout)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Errorf("a normal signed-out state says nothing: %q", stderr)
			}
		})
	}

	h := newHarness(t)
	registerPrism(t, h, filepath.Join(t.TempDir(), "gone"))
	h.mustRun(t, "accounts", "providers", "set", "prism")
	if _, stderr := h.mustRunStderr(t, "accounts"); strings.TrimSpace(stderr) != "" {
		t.Errorf("a launcher that isn't installed says nothing on a later run: %q", stderr)
	}
}

func TestLaunchWarnsOnABorrowedTokenThatRanOut(t *testing.T) {
	h := newHarness(t)
	prismAccounts(t, h, prismNotch)
	h.mustRun(t, "accounts", "providers", "set", "prism")

	signed, stderr, err := accountSession(t, h, "Jeb_")
	if err != nil {
		t.Fatalf("an expired borrowed account still launches: %v", err)
	}
	if signed.Minecraft == nil || signed.Minecraft.Token != "stale" {
		t.Errorf("the launch plays on the token it has: %+v", signed.Minecraft)
	}
	if !strings.Contains(stderr, "only prism can renew it") ||
		!strings.Contains(stderr, "online servers and Realms will reject this session") {
		t.Errorf("the launch has to say what won't work and who can fix it: %s", stderr)
	}
	// Shulker never writes another launcher's account into its own file, renewed or not.
	if _, err := os.Stat(account.Path(h.config)); !os.IsNotExist(err) {
		t.Errorf("a borrowed account must not land in shulker's own store: %v", err)
	}
	// A borrowed account that is still good says nothing.
	if _, stderr, err := accountSession(t, h, "Notch"); err != nil || strings.Contains(stderr, "Realms") {
		t.Errorf("a token that still holds launches quietly: %q %v", stderr, err)
	}
}
