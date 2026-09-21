package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceNameReadsTheLaunchersOwnFile(t *testing.T) {
	root := t.TempDir()

	prismGame := filepath.Join(root, "prism", "instances", "friends-pack", "minecraft")
	writeTestFile(t, filepath.Join(filepath.Dir(prismGame), PrismInstanceFile), "[General]\nConfigVersion=1.2\nname=Friends Pack\n")

	multimcGame := filepath.Join(root, "multimc", "instances", "friends-pack", ".minecraft")
	writeTestFile(t, filepath.Join(filepath.Dir(multimcGame), PrismInstanceFile), "name=Friends Pack\n")

	atGame := filepath.Join(root, "atlauncher", "instances", "FriendsPack")
	writeTestFile(t, filepath.Join(atGame, ATLauncherInstanceFile), `{"launcher":{"name":"Friends Pack"}}`)

	gdGame := filepath.Join(root, "gdlauncher", "instances", "Friends Pack", GDLauncherGameDir)
	writeTestFile(t, filepath.Join(filepath.Dir(gdGame), GDLauncherInstanceFile), `{"name":"Friends Pack"}`)

	mojangDir := filepath.Join(root, "minecraft")
	mojangGame := filepath.Join(mojangDir, "shulker", "anything")
	writeTestFile(t, filepath.Join(mojangDir, ProfilesFile), `{"profiles":{
		"player-made":{"name":"Not This","gameDir":"`+mojangGame+`"},
		"shulker-other":{"name":"Other Pack","gameDir":"`+filepath.Join(mojangDir, "elsewhere")+`"},
		"shulker-friends-pack":{"name":"Friends Pack","gameDir":"`+mojangGame+`"}
	}}`)

	for _, c := range []struct{ launcher, launcherDir, gameDir string }{
		{"prism", filepath.Join(root, "prism"), prismGame},
		{"multimc", filepath.Join(root, "multimc"), multimcGame},
		{"atlauncher", filepath.Join(root, "atlauncher"), atGame},
		{"gdlauncher", filepath.Join(root, "gdlauncher"), gdGame},
		{"mojang", mojangDir, mojangGame},
	} {
		if got := Find(c.launcher).InstanceName(c.launcherDir, c.gameDir); got != "Friends Pack" {
			t.Errorf("%s: InstanceName = %q", c.launcher, got)
		}
	}
}

// Repair heals, so a name it can't read is no failure: the caller falls back to what it has.
func TestInstanceNameIsEmptyWhenThereIsNothingToRead(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	writeTestFile(t, filepath.Join(broken, ATLauncherInstanceFile), "{ not json")
	writeTestFile(t, filepath.Join(root, ProfilesFile), `{"profiles":{"shulker-x":{"name":"X","gameDir":"/elsewhere"}}}`)

	for _, c := range []struct{ launcher, launcherDir, gameDir string }{
		{"prism", root, filepath.Join(root, "absent", "minecraft")},
		{"atlauncher", root, broken},
		{"gdlauncher", root, filepath.Join(root, "absent", GDLauncherGameDir)},
		{"mojang", root, filepath.Join(root, "deleted-profile")},
		{"mojang", "", filepath.Join(root, "deleted-profile")},
		{"shulker", "", filepath.Join(root, "absent")},
	} {
		if got := Find(c.launcher).InstanceName(c.launcherDir, c.gameDir); got != "" {
			t.Errorf("%s: InstanceName = %q, want none", c.launcher, got)
		}
	}
	if got := (*Entry)(nil).InstanceName(root, root); got != "" {
		t.Errorf("no launcher: InstanceName = %q", got)
	}
}
