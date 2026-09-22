package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modpackDefaults = "config/modpack_defaults/options.txt"

func setOptionsPath(t *testing.T, h *harness, rel string) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		client, _ := m["client"].(map[string]any)
		if client == nil {
			client = map[string]any{}
		}
		client["optionsPath"] = rel
		m["client"] = client
	})
}

func TestOptionsPathMovesTheBuiltOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	setOptionsPath(t, h, modpackDefaults)
	h.mustRun(t, "install")

	got := readBuilt(t, h, modpackDefaults)
	if !strings.Contains(got, "tutorialStep:none") || !strings.Contains(got, `resourcePacks:["vanilla","file/fresh-animations.zip"]`) {
		t.Fatalf("%s: %q", modpackDefaults, got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "options.txt")); !os.IsNotExist(err) {
		t.Fatalf("options.txt was written as well: %v", err)
	}

	h.mustRun(t, "shader", "add", "complementary-reimagined")
	options := filepath.Join(h.dir, "build", "client", filepath.FromSlash(modpackDefaults))
	if err := os.WriteFile(options, []byte("fov:0.5\nresourcePacks:[\"vanilla\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if got := readBuilt(t, h, modpackDefaults); !strings.Contains(got, "resourcePacks:[\"vanilla\"]\n") || !strings.Contains(got, "fov:0.5") {
		t.Fatalf("the player's list at the options path was not held: %q", got)
	}
}

func TestOptionsPathSeedsOverAnUntouchedList(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	setOptionsPath(t, h, modpackDefaults)
	writeOverride(t, h.dir, "build/client/options.txt", "resourcePacks:[\"vanilla\",\"file/mine.zip\"]\n")
	writeOverride(t, h.dir, "build/client/"+modpackDefaults, "resourcePacks:[\"vanilla\"]\n")
	h.mustRun(t, "install")

	if got := readBuilt(t, h, modpackDefaults); !strings.Contains(got, `resourcePacks:["vanilla","file/fresh-animations.zip"]`) {
		t.Fatalf("%s: %q", modpackDefaults, got)
	}
	if got := readBuilt(t, h, "options.txt"); got != "resourcePacks:[\"vanilla\",\"file/mine.zip\"]\n" {
		t.Fatalf("the game's own options.txt was touched: %q", got)
	}
}

func TestOptionsPathInExports(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations", "--provider", "curseforge")
	setOptionsPath(t, h, modpackDefaults)
	h.mustRun(t, "install")
	h.allowMrpackHost(t)

	h.mustRun(t, "export", "mrpack", "--version", "1.0")
	_, entries := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if got := entries["overrides/"+modpackDefaults]; !strings.Contains(got, `"file/fresh-animations.zip"`) {
		t.Fatalf("mrpack %s: %q (entries: %v)", modpackDefaults, got, keys(entries))
	}
	if _, ok := entries["overrides/options.txt"]; ok {
		t.Fatal("the mrpack carries options.txt as well")
	}

	h.mustRun(t, "export", "curseforge", "--version", "1.0")
	entries = readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	if got := entries["overrides/"+modpackDefaults]; !strings.Contains(got, `"file/FreshAnimations_CF_v1.9.4.zip"`) || strings.Contains(got, "fresh-animations.zip") {
		t.Fatalf("curseforge %s: %q", modpackDefaults, got)
	}
	if _, ok := entries["overrides/options.txt"]; ok {
		t.Fatal("the curseforge profile carries options.txt as well")
	}
}

func TestOptionsPathStaysInTheBuild(t *testing.T) {
	for _, rel := range []string{"/etc/options.txt", `C:\options.txt`, "C:/options.txt", "../options.txt", "config/../../options.txt", ".", "config/.."} {
		t.Run(rel, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
			setOptionsPath(t, h, rel)
			code, stdout, _ := h.run(t, "build", "--json")
			if code == 0 || !strings.Contains(stdout, "manifest-invalid") {
				t.Fatalf("exit %d: %s", code, stdout)
			}
		})
	}
}
