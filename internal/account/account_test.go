package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"shulker.sh/shulker/internal/out"
)

func own(name, id string) Account {
	return Account{Type: Microsoft, Profile: &Profile{ID: id, Name: name}, RefreshToken: "r-" + id}
}

func offline(name, id string) Account {
	return Account{Type: Offline, Profile: &Profile{ID: id, Name: name}}
}

func TestIDAndName(t *testing.T) {
	gamertag := Account{Type: Microsoft, Xbox: &Xbox{XUID: "2533274800000000", Gamertag: "Big Dog 42"}, RefreshToken: "r"}
	if got := gamertag.ID(); got != "2533274800000000" {
		t.Errorf("ID = %q, want the XUID", got)
	}
	if got := gamertag.Name(); got != "Big Dog 42" {
		t.Errorf("Name = %q, want the gamertag", got)
	}
	notch := own("Notch", "069a79f4-44e9-4726-a5be-fca90e38aaf5")
	if notch.ID() != "069a79f4-44e9-4726-a5be-fca90e38aaf5" || notch.Name() != "Notch" {
		t.Errorf("profile should win: %q %q", notch.ID(), notch.Name())
	}
}

func TestState(t *testing.T) {
	for _, c := range []struct {
		name string
		a    Account
		want State
	}{
		{"playable", own("Notch", "u1"), Playable},
		{"no refresh token", Account{Type: Microsoft, Profile: &Profile{ID: "u1", Name: "Notch"}}, SignInExpired},
		{"no profile", Account{Type: Microsoft, Xbox: &Xbox{XUID: "x", Gamertag: "g"}, RefreshToken: "r"}, NoProfile},
		{"offline", offline("Steve", "u2"), OfflineOnly},
	} {
		if got := c.a.State(); got != c.want {
			t.Errorf("%s: State = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStateText(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		s    State
		when time.Time
		want string
	}{
		{Playable, time.Time{}, "playable"},
		{NoProfile, time.Time{}, "not playable (no Java profile)"},
		{SignInExpired, time.Time{}, "sign-in expired"},
		{OfflineOnly, time.Time{}, "offline"},
		{TokenExpired, now.Add(-72 * time.Hour), "token expired 3 days ago"},
		{TokenExpired, now.Add(-90 * time.Minute), "token expired 1 hour ago"},
		{TokenExpired, time.Time{}, "token expired"},
	} {
		if got := c.s.Text(c.when, now); got != c.want {
			t.Errorf("%q.Text = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestSaveWritesSchemaLineAt0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker", "accounts.json")
	if err := Save(path, Store{Accounts: []Account{own("Notch", "u1")}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatal(err)
	}
	if head.Schema != "https://shulker.sh/schema/v1/accounts.json" {
		t.Errorf("$schema = %q", head.Schema)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != Mode {
			t.Errorf("mode = %v, want %v", got, Mode)
		}
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	want := Store{Accounts: []Account{
		own("Notch", "069a79f4-44e9-4726-a5be-fca90e38aaf5"),
		offline("Steve", "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"),
	}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 2 || got.Accounts[0].Name() != "Notch" || got.Accounts[1].Type != Offline {
		t.Fatalf("round trip lost accounts: %+v", got.Accounts)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil || len(got.Accounts) != 0 {
		t.Fatalf("Load of a missing file = %+v, %v", got, err)
	}
}

func TestLoadRejectsAForeignFile(t *testing.T) {
	for _, c := range []struct{ name, data, code string }{
		{"no marker", `{"accounts":[]}`, "accounts-invalid"},
		{"newer", `{"$schema":"https://shulker.sh/schema/v2/accounts.json"}`, "schema-newer"},
		{"unknown key", `{"$schema":"https://shulker.sh/schema/v1/accounts.json","nope":1}`, "accounts-invalid"},
		{"bad type", `{"$schema":"https://shulker.sh/schema/v1/accounts.json","accounts":[{"type":"steam"}]}`, "accounts-invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.json")
			if err := os.WriteFile(path, []byte(c.data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if got := out.CodeOf(err); got != c.code {
				t.Fatalf("code = %q, want %q (%v)", got, c.code, err)
			}
		})
	}
}

func TestResolveDedupesByIDKeepingTheEarliestProvider(t *testing.T) {
	store := Store{Accounts: []Account{
		own("Notch", "069a79f4-44e9-4726-a5be-fca90e38aaf5"),
		own("NOTCH", "069A79F444E94726A5BEFCA90E38AAF5"),
		offline("Steve", "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"),
	}}
	got := Resolve(DefaultProviders(), store, nil)
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2: %+v", len(got), got)
	}
	if got[0].Source != SourceShulker || got[0].Group != GroupOwn {
		t.Errorf("own account = %+v", got[0])
	}
	if got[1].Source != SourceOffline || got[1].Group != GroupOffline {
		t.Errorf("offline account = %+v", got[1])
	}
}

func TestResolveIgnoresAProviderThatFoundNothing(t *testing.T) {
	store := Store{Accounts: []Account{own("Notch", "u1")}}
	if got := Resolve([]string{SourceMojang}, store, nil); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
	if got := Resolve(nil, store, nil); len(got) != 0 {
		t.Fatalf("an empty provider list should yield nothing, got %+v", got)
	}
}

func TestReadersForEveryProviderTheListTakes(t *testing.T) {
	// A launcher may be offered before its reader exists, so a list can drop shulker for one of
	// them; today every name accounts.providers accepts has one.
	for _, p := range Providers() {
		if !HasReader(p) {
			t.Errorf("%s is offered without a reader", p)
		}
	}
	if len(WithoutReader(Providers())) != 0 {
		t.Errorf("without a reader = %v", WithoutReader(Providers()))
	}
	if HasReader("multimc") {
		t.Error("a launcher the list doesn't take has no reader either")
	}
	if got := WithoutReader([]string{SourceShulker, "multimc"}); len(got) != 1 || got[0] != "multimc" {
		t.Errorf("without a reader = %v, want the one that has none", got)
	}
}

func TestResolveTakesTheEarliestProvidersCopyOfAnAccount(t *testing.T) {
	id := "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	borrowed := map[string][]Resolved{SourcePrism: {{
		ID: "069A79F444E94726A5BEFCA90E38AAF5", Name: "Notch",
		Source: SourcePrism, Group: GroupBorrowed, State: TokenExpired,
	}}}
	store := Store{Accounts: []Account{own("Notch", id)}}

	got := Resolve([]string{SourceShulker, SourcePrism}, store, borrowed)
	if len(got) != 1 || got[0].Source != SourceShulker {
		t.Fatalf("shulker comes first, so its copy wins: %+v", got)
	}
	got = Resolve([]string{SourcePrism, SourceShulker}, store, borrowed)
	if len(got) != 1 || got[0].Source != SourcePrism {
		t.Fatalf("prism comes first, so its copy wins: %+v", got)
	}
	if got = Resolve([]string{SourcePrism}, store, borrowed); len(got) != 1 || got[0].State != TokenExpired {
		t.Fatalf("without shulker only the borrowed copy is left: %+v", got)
	}
}

func TestFind(t *testing.T) {
	accounts := Resolve(DefaultProviders(), Store{Accounts: []Account{
		own("Notch", "069a79f4-44e9-4726-a5be-fca90e38aaf5"),
		offline("Notch", "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"),
		offline("Big Dog 42", "1111ba71-b85a-3d5b-af5f-cb2f6e9c7d21"),
	}}, nil)
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"Notch", []string{"069a79f4-44e9-4726-a5be-fca90e38aaf5", "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"}},
		{"nOtCh", []string{"069a79f4-44e9-4726-a5be-fca90e38aaf5", "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"}},
		{"Notch@shulker", []string{"069a79f4-44e9-4726-a5be-fca90e38aaf5"}},
		{"Notch@offline", []string{"8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"}},
		{"069a79f4-44e9-4726-a5be-fca90e38aaf5", []string{"069a79f4-44e9-4726-a5be-fca90e38aaf5"}},
		{"069A79F444E94726A5BEFCA90E38AAF5", []string{"069a79f4-44e9-4726-a5be-fca90e38aaf5"}},
		{"Big Dog 42", []string{"1111ba71-b85a-3d5b-af5f-cb2f6e9c7d21"}},
		{"Dinnerbone", nil},
		{"Notch@prism", nil},
	} {
		var got []string
		for _, a := range Find(accounts, c.query) {
			got = append(got, a.ID)
		}
		if len(got) != len(c.want) {
			t.Errorf("Find(%q) = %v, want %v", c.query, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Find(%q) = %v, want %v", c.query, got, c.want)
				break
			}
		}
	}
}

func TestQualifier(t *testing.T) {
	r := Resolved{Name: "Notch", Source: SourceOffline}
	if got := r.Qualifier(); got != "Notch@offline" {
		t.Errorf("Qualifier = %q", got)
	}
}

func TestLoadRejectsAnAccountItCouldNotName(t *testing.T) {
	for _, c := range []struct{ name, entry string }{
		{"nothing to identify it", `{"type":"microsoft","refreshToken":"r"}`},
		{"offline with no profile", `{"type":"offline","xbox":{"xuid":"2533274800000000"}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.json")
			data := `{"$schema":"https://shulker.sh/schema/v1/accounts.json","accounts":[` + c.entry + `]}`
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); out.CodeOf(err) != "accounts-invalid" {
				t.Fatalf("Load = %v, want accounts-invalid", err)
			}
		})
	}
}
