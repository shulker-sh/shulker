package launcher

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrismAndMultiMCKeepTheInstanceIconInTheIconsFolder(t *testing.T) {
	for _, tc := range []struct {
		entry *Entry
		link  func(dir string) (InstanceResult, error)
		file  string
	}{
		{prismEntry, func(dir string) (InstanceResult, error) {
			return (&Prism{Dir: dir}).WriteInstance(PrismInstance{ID: "shulker-smp", Name: "SMP", Minecraft: "26.2"})
		}, PrismInstanceFile},
		{multimcEntry, func(dir string) (InstanceResult, error) {
			return (&MultiMC{Dir: dir}).WriteInstance(MultiMCInstance{ID: "shulker-smp", Name: "SMP", Minecraft: "26.2"})
		}, MultiMCInstanceFile},
	} {
		t.Run(tc.entry.Name, func(t *testing.T) {
			dir := t.TempDir()
			res, err := tc.link(dir)
			if err != nil {
				t.Fatal(err)
			}
			iconFile := filepath.Join(dir, "icons", "shulker-smp.png")
			if data, _ := os.ReadFile(iconFile); !bytes.Equal(data, Icon) {
				t.Fatalf("a link writes shulker's icon to %s", iconFile)
			}
			cfgPath := filepath.Join(res.Dir, tc.file)
			if cfg, _ := os.ReadFile(cfgPath); !strings.Contains(string(cfg), "iconKey=shulker-smp\n") {
				t.Fatalf("a link names the icon by the instance's folder:\n%s", cfg)
			}

			in := config.Instance{Launcher: tc.entry.Name, LauncherDir: dir, Dir: res.GameDir}
			pack := testPNG(t, 64, 64)
			hash, err := tc.entry.SyncImage(in, pack, "")
			if err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(iconFile); !bytes.Equal(data, pack) || hash != imageHash(pack) {
				t.Fatal("a sync writes the pack icon over the default")
			}

			picked := testPNG(t, 8, 8)
			if err := os.WriteFile(iconFile, picked, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.entry.SyncImage(in, pack, hash); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(iconFile); !bytes.Equal(data, picked) {
				t.Fatal("an unchanged pack icon leaves the file alone")
			}

			if _, err := tc.entry.SyncImage(config.Instance{Launcher: tc.entry.Name, Dir: res.GameDir}, testPNG(t, 4, 4), hash); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(iconFile); !bytes.Equal(data, picked) {
				t.Fatal("a row with no launcher directory is left alone")
			}

			cfg, _ := os.ReadFile(cfgPath)
			if err := os.WriteFile(cfgPath, bytes.Replace(cfg, []byte("iconKey=shulker-smp"), []byte("iconKey=flame"), 1), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.link(dir); err != nil {
				t.Fatal(err)
			}
			if cfg, _ := os.ReadFile(cfgPath); !strings.Contains(string(cfg), "iconKey=flame\n") || strings.Contains(string(cfg), "iconKey=shulker-smp") {
				t.Fatalf("a relink keeps an icon the player picked:\n%s", cfg)
			}
			if data, _ := os.ReadFile(iconFile); !bytes.Equal(data, picked) {
				t.Fatal("a relink leaves the icon file alone")
			}

			if err := os.WriteFile(cfgPath, bytes.Replace(cfg, []byte("iconKey=flame"), []byte("iconKey=default"), 1), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.link(dir); err != nil {
				t.Fatal(err)
			}
			if cfg, _ := os.ReadFile(cfgPath); !strings.Contains(string(cfg), "iconKey=shulker-smp\n") {
				t.Fatalf("a relink takes over the launcher's default icon:\n%s", cfg)
			}
		})
	}
}

func TestPrismIconsFolderFollowsItsSetting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prismlauncher.cfg"), []byte("[General]\nIconsDir=pictures\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Prism{Dir: dir}).WriteInstance(PrismInstance{ID: "shulker-smp", Name: "SMP", Minecraft: "26.2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pictures", "shulker-smp.png")); err != nil {
		t.Fatal(err)
	}
}

func TestMojangKeepsTheProfileIconInline(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "shulker", "smp")
	m := &Mojang{Dir: dir}
	icon := func(key string) string {
		t.Helper()
		profiles, err := m.Profiles()
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Icon string `json:"icon"`
		}
		if err := json.Unmarshal(profiles[key], &p); err != nil {
			t.Fatal(err)
		}
		return p.Icon
	}
	if err := m.WriteProfile(Profile{Key: "shulker-smp", Name: "SMP", VersionID: "26.2", GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteProfile(Profile{Key: "other", Name: "Other", VersionID: "26.2", GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	if icon("shulker-smp") != mojangIconValue(Icon) {
		t.Fatal("a link gives the profile shulker's icon")
	}

	in := config.Instance{Launcher: "mojang", LauncherDir: dir, Dir: gameDir}
	hash, err := mojangEntry.SyncImage(in, testPNG(t, 64, 32), "")
	if err != nil {
		t.Fatal(err)
	}
	data, found := strings.CutPrefix(icon("shulker-smp"), "data:image/png;base64,")
	if !found || icon("other") != mojangIconValue(Icon) {
		t.Fatalf("a sync writes the pack icon inline, on shulker's profile only: %q", icon("shulker-smp"))
	}
	fitted, err := mojangIcon(testPNG(t, 64, 32))
	if err != nil {
		t.Fatal(err)
	}
	if data != strings.TrimPrefix(mojangIconValue(fitted), "data:image/png;base64,") || hash != imageHash(fitted) {
		t.Fatal("the profile icon is the pack icon fitted")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(fitted))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("the fitted icon is %dx%d, %v", cfg.Width, cfg.Height, err)
	}

	if err := m.setProfileKey(gameDir, "icon", "Furnace"); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteProfile(Profile{Key: "shulker-smp", Name: "SMP", VersionID: "26.3", GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := mojangEntry.SyncImage(in, testPNG(t, 64, 32), hash); err != nil {
		t.Fatal(err)
	}
	if icon("shulker-smp") != "Furnace" {
		t.Fatalf("an icon the player picked survives a relink and a sync: %q", icon("shulker-smp"))
	}
}
