package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	got := Resolve(DefaultStores(), store, nil)
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

func TestResolveIgnoresAStoreThatFoundNothing(t *testing.T) {
	store := Store{Accounts: []Account{own("Notch", "u1")}}
	if got := Resolve([]string{"mojang"}, store, nil); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
	if got := Resolve(nil, store, nil); len(got) != 0 {
		t.Fatalf("an empty store list should yield nothing, got %+v", got)
	}
}

func TestResolveTakesTheEarliestStoresCopyOfAnAccount(t *testing.T) {
	id := "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	fromLaunchers := map[string][]Resolved{"prism": {{
		ID: "069A79F444E94726A5BEFCA90E38AAF5", Name: "Notch",
		Source: "prism", Group: GroupLauncher, State: TokenExpired,
	}}}
	store := Store{Accounts: []Account{own("Notch", id)}}

	got := Resolve([]string{SourceShulker, "prism"}, store, fromLaunchers)
	if len(got) != 1 || got[0].Source != SourceShulker {
		t.Fatalf("shulker comes first, so its copy wins: %+v", got)
	}
	got = Resolve([]string{"prism", SourceShulker}, store, fromLaunchers)
	if len(got) != 1 || got[0].Source != "prism" {
		t.Fatalf("prism comes first, so its copy wins: %+v", got)
	}
	if got = Resolve([]string{"prism"}, store, fromLaunchers); len(got) != 1 || got[0].State != TokenExpired {
		t.Fatalf("without shulker only the launcher copy is left: %+v", got)
	}
}

func TestFind(t *testing.T) {
	const (
		notch   = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
		offNotc = "8667ba71-b85a-3d5b-af5f-cb2f6e9c7d21"
		bigDog  = "1111ba71-b85a-3d5b-af5f-cb2f6e9c7d21"
		upSteve = "5a1e0000-0000-3000-8000-000000000001"
		loSteve = "5a1f0000-0000-3000-8000-000000000002"
		stella  = "b1a00000-0000-3000-8000-000000000003"
	)
	accounts := Resolve(DefaultStores(), Store{Accounts: []Account{
		own("Notch", notch),
		offline("Notch", offNotc),
		offline("Big Dog 42", bigDog),
		offline("Steve", upSteve),
		offline("steve", loSteve),
		offline("Stella", stella),
	}}, nil)
	for _, c := range []struct {
		query   string
		want    []string
		closest bool
	}{
		{"Notch", []string{notch, offNotc}, false},
		{"nOtCh", []string{notch, offNotc}, false},
		{"Notch@shulker", []string{notch}, false},
		{"Notch@offline", []string{offNotc}, false},
		{"Not@offline", []string{offNotc}, false},
		{notch, []string{notch}, false},
		{"069A79F444E94726A5BEFCA90E38AAF5", []string{notch}, false},
		{"Big Dog 42", []string{bigDog}, false},
		{"big", []string{bigDog}, false},
		{"steve", []string{loSteve, upSteve}, true},
		{"Steve@offline", []string{upSteve, loSteve}, true},
		{"ste", []string{loSteve, upSteve, stella}, true},
		{"Ste", []string{upSteve, stella, loSteve}, true},
		{"S", []string{upSteve, stella, loSteve}, true},
		{"STE", []string{upSteve, loSteve, stella}, true},
		{"5a1", []string{upSteve, loSteve}, false},
		{"5A1E-0", []string{upSteve}, false},
		{"b", []string{bigDog}, false},
		{"b1", []string{stella}, false},
		{"8", []string{offNotc}, false},
		{"Dinnerbone", nil, false},
		{"Notch@prism", nil, false},
		{"", nil, false},
	} {
		matches, closest := Find(accounts, c.query)
		var got []string
		for _, a := range matches {
			got = append(got, a.ID)
		}
		if !slices.Equal(got, c.want) || closest != c.closest {
			t.Errorf("Find(%q) = %v, %v; want %v, %v", c.query, got, closest, c.want, c.closest)
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

func TestResolvedOfAnAccountIsOwnOrOfflineByItsType(t *testing.T) {
	own := Account{Type: Microsoft, Profile: &Profile{ID: "1111", Name: "Steve"}, RefreshToken: "r"}
	r := own.Resolved()
	if r.ID != "1111" || r.Name != "Steve" || r.Source != SourceShulker || r.Group != GroupOwn || r.State != Playable || !r.IsOwn() {
		t.Fatalf("own = %+v", r)
	}
	o := NewOffline("Alex", "2222").Resolved()
	if o.Source != SourceOffline || o.Group != GroupOffline || o.State != OfflineOnly || o.IsOwn() {
		t.Fatalf("offline = %+v", o)
	}
}

func TestOwnAndWithoutProfileFilterTheList(t *testing.T) {
	accounts := []Resolved{
		{ID: "1", Source: SourceShulker, State: Playable},
		{ID: "2", Source: SourceShulker, State: NoProfile},
		{ID: "3", Source: "prism", State: Playable},
		{ID: "4", Source: SourceOffline, State: OfflineOnly},
	}
	ids := func(rs []Resolved) []string {
		var got []string
		for _, r := range rs {
			got = append(got, r.ID)
		}
		return got
	}
	if got := ids(Own(accounts)); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("own = %v", got)
	}
	if got := ids(WithoutProfile(accounts)); !slices.Equal(got, []string{"2"}) {
		t.Fatalf("without profile = %v", got)
	}
	if got, ok := ByID(accounts, "3"); !ok || got.Source != "prism" {
		t.Fatalf("by id = %+v, %v", got, ok)
	}
	if _, ok := ByID(accounts, "9"); ok {
		t.Fatal("an unknown id is not found")
	}
}

func TestIsDefaultMatchesTheConfiguredIDInEitherForm(t *testing.T) {
	r := Resolved{ID: "069a79f4-44e9-4726-a5be-fca90e38aaf5"}
	if !r.IsDefault("069a79f444e94726a5befca90e38aaf5") {
		t.Fatal("dashes don't matter")
	}
	if r.IsDefault("") || r.IsDefault("other") {
		t.Fatal("no default, or another id, is not this account")
	}
}

func TestSelectorsNameAnAccountByIDOnlyWhenItsNameIsShared(t *testing.T) {
	steve := Resolved{ID: "id-steve", Name: "Steve", Source: SourceOffline}
	alex := Resolved{ID: "id-alex", Name: "Alex", Source: SourceOffline}
	spaced := Resolved{ID: "id-spaced", Name: "Big Steve", Source: SourceOffline}
	all := []Resolved{steve, alex, spaced, {ID: "id-alex-2", Name: "Alex", Source: "prism"}}
	got := Selectors(all, []Resolved{steve, alex, spaced})
	want := []string{"Steve", "id-alex", `"Big Steve"`}
	if !slices.Equal(got, want) {
		t.Fatalf("Selectors = %q, want %q", got, want)
	}
}

func TestRemovableKeepsOnlyOfflineAccounts(t *testing.T) {
	steve := Resolved{ID: "id-steve", Name: "Steve", Source: SourceOffline}
	all := []Resolved{{ID: "id-notch", Name: "Notch", Source: SourceShulker}, steve, {ID: "id-alex", Name: "Alex", Source: "prism"}}
	if got := Removable(all); len(got) != 1 || got[0] != steve {
		t.Fatalf("Removable = %+v", got)
	}
	if len(all) != 3 {
		t.Fatalf("Removable changed its input: %+v", all)
	}
}
