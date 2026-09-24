package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

func TestPlacePutsEachLauncherInstanceWhereItNamesIt(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		entry        *Entry
		dir, gameDir string
		id           string
	}{
		{Shulker, "friends-smp", "friends-smp", "friends-smp"},
		{prismEntry, "instances/shulker-friends-smp", "instances/shulker-friends-smp/minecraft", ""},
		{multimcEntry, "instances/shulker-friends-smp", "instances/shulker-friends-smp/minecraft", ""},
		{mojangEntry, "shulker/friends-smp", "shulker/friends-smp", ""},
		{atlauncherEntry, "instances/FriendsSMP", "instances/FriendsSMP", ""},
		{gdlauncherEntry, "instances/Friends SMP", "instances/Friends SMP/instance", ""},
	}
	for _, c := range cases {
		got, err := c.entry.Place(&Link{LauncherDir: root, Name: "Friends SMP"})
		if err != nil {
			t.Fatalf("%s: %v", c.entry.Name, err)
		}
		want := Placement{ID: c.id, Dir: filepath.Join(root, c.dir), GameDir: filepath.Join(root, c.gameDir)}
		if got != want {
			t.Errorf("%s placed at %+v, want %+v", c.entry.Name, got, want)
		}
	}
}

func TestPlaceRefusesANameTheLauncherFoldsToNothing(t *testing.T) {
	for _, c := range []struct {
		entry *Entry
		name  string
	}{{atlauncherEntry, "…"}, {gdlauncherEntry, "  "}} {
		_, err := c.entry.Place(&Link{LauncherDir: t.TempDir(), Name: c.name})
		if out.CodeOf(err) != "usage" {
			t.Errorf("%s accepted %q: %v", c.entry.Name, c.name, err)
		}
	}
}

func TestShulkerNickSuffixesUntilFreeOrOwn(t *testing.T) {
	root := t.TempDir()
	taken := []config.Instance{{ID: "pack", Dir: filepath.Join(root, "elsewhere")}, {ID: "pack-2", Dir: filepath.Join(root, "pack-2")}}
	if got := shulkerNick(taken, root, "", "Pack"); got != "pack-2" {
		t.Errorf("nick = %q, want the suffix that already names its own folder", got)
	}
	if got := shulkerNick(taken, root, "mine", "Pack"); got != "mine" {
		t.Errorf("nick = %q, want --as as given", got)
	}
}

func TestLocateFailsWhereNoLauncherIs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere")
	if _, err := prismEntry.Locate(missing); out.CodeOf(err) != "launcher-not-found" {
		t.Fatalf("err = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	real := t.TempDir()
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	got, err := gdlauncherEntry.Locate(link)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if got != want {
		t.Errorf("GDLauncher located %q, want the resolved %q", got, want)
	}
}

func TestAccountStoresMatchTheConfigSchema(t *testing.T) {
	raw, err := schema.Raw(schema.Config)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			Accounts struct {
				Properties struct {
					Stores struct {
						Items struct {
							Enum []string `json:"enum"`
						} `json:"items"`
					} `json:"stores"`
				} `json:"properties"`
			} `json:"accounts"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc.Properties.Accounts.Properties.Stores.Items.Enum; !slices.Equal(got, AccountStores()) {
		t.Errorf("schema accepts %v, the entries offer %v", got, AccountStores())
	}
}
