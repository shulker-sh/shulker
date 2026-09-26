package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
)

func TestClientBuildWritesMarkerJar(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "my.pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	jarPath := filepath.Join(h.dir, "build", "client", "mods", "shulker-my.pack.jar")
	first, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := readZip(t, first)
	var meta struct {
		SchemaVersion int    `json:"schemaVersion"`
		ID            string `json:"id"`
		Version       string `json:"version"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Icon          string `json:"icon"`
		Environment   string `json:"environment"`
		Entrypoints   struct {
			ModMenu []string `json:"modmenu"`
		} `json:"entrypoints"`
		Custom struct {
			ModMenu struct {
				UpdateChecker bool `json:"update_checker"`
			} `json:"modmenu"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(entries["fabric.mod.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SchemaVersion != 1 || meta.ID != "shulker_my_pack" || meta.Name != "my.pack" || meta.Environment != "*" || !strings.HasPrefix(meta.Version, "0.0.0+") || len(meta.Version) != len("0.0.0+")+8 || meta.Custom.ModMenu.UpdateChecker {
		t.Fatalf("fabric.mod.json: %+v", meta)
	}
	want := "Minecraft 26.2 \u2022 fabric 0.17.3 \u2022 2 mods\n\n<bold>Mods</bold>\n  \u2022 sodium\n\n<bold>Dependencies</bold>\n  \u2022 fabric-api"
	if meta.Description != want {
		t.Fatalf("description:\n%s", meta.Description)
	}
	if meta.Icon != "assets/shulker_my_pack/icon.png" || len(entries[meta.Icon]) == 0 {
		t.Fatalf("icon %q missing from jar", meta.Icon)
	}
	if len(meta.Entrypoints.ModMenu) != 1 || meta.Entrypoints.ModMenu[0] != "shulker.marker.ShulkerModMenu" {
		t.Fatalf("entrypoints: %+v", meta.Entrypoints)
	}
	if got := string(entries["shulker/mods.txt"]); got != "fabric-api\nsodium\n" {
		t.Fatalf("mods.txt: %q", got)
	}
	for _, class := range []string{"shulker/marker/ShulkerModMenu.class", "shulker/marker/ShulkerModMenu$1.class"} {
		data := entries[class]
		if len(data) < 8 || !bytes.HasPrefix(data, []byte{0xCA, 0xFE, 0xBA, 0xBE}) {
			t.Fatalf("%s missing or not a class file", class)
		}
		if major := int(data[6])<<8 | int(data[7]); major != 61 {
			t.Fatalf("%s is class version %d, want 61 (Java 17)", class, major)
		}
	}
	var embedded map[string]any
	if err := json.Unmarshal(entries["shulker.lock"], &embedded); err != nil || embedded["minecraft"] != "26.2" {
		t.Fatalf("embedded lock: %v %v", err, embedded)
	}
	if err := json.Unmarshal(entries["shulker.json"], &embedded); err != nil || embedded["name"] != "my.pack" {
		t.Fatalf("embedded manifest: %v %v", err, embedded)
	}

	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "Built client (4 unchanged)") {
		t.Fatalf("rebuild should be a no-op: %s", stdout)
	}
	second, _ := os.ReadFile(jarPath)
	if !bytes.Equal(first, second) {
		t.Fatal("marker jar is not byte-stable across builds")
	}
}

func readZip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return entries
}

func TestMarkerJarCarriesAuthorsAndLinks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "linked")
	h.editManifest(t, func(m map[string]any) {
		m["description"] = "Survival with friends."
		m["authors"] = []string{"Alice", "shulker.sh"}
		m["license"] = "MIT"
		m["links"] = map[string]any{
			"website":      "https://example.com",
			"issues":       "https://example.com/issues",
			"source":       "https://example.com/src",
			"discord":      "https://discord.gg/abc",
			"Server rules": "https://example.com/rules",
		}
	})
	h.mustRun(t, "install")

	data, err := os.ReadFile(filepath.Join(h.dir, "build", "client", "mods", "shulker-linked.jar"))
	if err != nil {
		t.Fatal(err)
	}
	entries := readZip(t, data)
	var meta struct {
		Description string            `json:"description"`
		Authors     []string          `json:"authors"`
		License     string            `json:"license"`
		Contact     map[string]string `json:"contact"`
		Custom      struct {
			ModMenu struct {
				Links map[string]string `json:"links"`
			} `json:"modmenu"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(entries["fabric.mod.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(meta.Description, "Survival with friends.\n\nMinecraft 26.2 \u2022 ") {
		t.Fatalf("description:\n%s", meta.Description)
	}
	if strings.Join(meta.Authors, ",") != "Alice,shulker.sh" {
		t.Fatalf("authors: %v", meta.Authors)
	}
	if meta.License != "MIT" {
		t.Fatalf("license: %q", meta.License)
	}
	wantContact := map[string]string{"homepage": "https://example.com", "issues": "https://example.com/issues", "sources": "https://example.com/src"}
	if fmt.Sprint(meta.Contact) != fmt.Sprint(wantContact) {
		t.Fatalf("contact: %v", meta.Contact)
	}
	wantLinks := map[string]string{"modmenu.discord": "https://discord.gg/abc", "shulker.link.server_rules": "https://example.com/rules"}
	if fmt.Sprint(meta.Custom.ModMenu.Links) != fmt.Sprint(wantLinks) {
		t.Fatalf("links: %v", meta.Custom.ModMenu.Links)
	}
	var lang map[string]string
	if err := json.Unmarshal(entries["assets/shulker_linked/lang/en_us.json"], &lang); err != nil || lang["shulker.link.server_rules"] != "Server rules" {
		t.Fatalf("lang: %v %v", err, lang)
	}
}

func TestMarkerUsesManifestVersionAndDisplayName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "versioned")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["client"].(map[string]any)["name"] = "LAN Party"
	})
	h.mustRun(t, "install")
	data, err := os.ReadFile(filepath.Join(h.dir, "build", "client", "mods", "shulker-versioned.jar"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(readZip(t, data)["fabric.mod.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(meta.Version, "1.0+") || len(meta.Version) != len("1.0+")+8 {
		t.Fatalf("version: %q", meta.Version)
	}
	if meta.Name != "LAN Party" || meta.ID != "shulker_versioned" {
		t.Fatalf("name %q id %q", meta.Name, meta.ID)
	}
}

func TestInitSeedsAuthors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "seeded")
	m := h.readManifest(t)
	if len(m.Authors) == 0 || m.Authors[len(m.Authors)-1] != "shulker.sh" {
		t.Fatalf("authors: %v", m.Authors)
	}
}

// TestMarkerSwitchResolvesTheSameWayOnEveryBuild pins the layers: the directory's own
// settings.marker where it names one, else the manifest's, else on. A sync writes the instance
// file after the build, so a seeded value would make the first run into a directory disagree with
// every run after.
func TestMarkerSwitchResolvesTheSameWayOnEveryBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) { m["marker"] = false })
	h.mustRun(t, "install")

	into := t.TempDir()
	jarIn := func(dir string) string { return filepath.Join(dir, "mods", "shulker-pack.jar") }
	missing := func(t *testing.T, what, dir string) {
		t.Helper()
		if _, err := os.Stat(jarIn(dir)); !os.IsNotExist(err) {
			t.Fatalf("%s: the manifest leaves the marker out: %v", what, err)
		}
	}
	present := func(t *testing.T, what, dir string) {
		t.Helper()
		if _, err := os.Stat(jarIn(dir)); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	h.mustRun(t, "sync", h.dir, "--into", into)
	missing(t, "first sync", into)
	if f := readIntent(t, into); f.Settings.Marker != nil {
		t.Fatalf("a sync writes no marker value of its own: %+v", f.Settings)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	missing(t, "second sync", into)

	// Every other build site reads the same layers.
	missing(t, "install", filepath.Join(h.dir, "build", "client"))
	h.mustRun(t, "build")
	missing(t, "build", filepath.Join(h.dir, "build", "client"))
	if stdout := h.mustRun(t, "diff", "client", "--into", into); !strings.Contains(stdout, "No changes") {
		t.Fatalf("diff sees no drift from a marker nobody asked for: %s", stdout)
	}
	h.mustRun(t, "pull", "--into", into)
	missing(t, "pull", into)

	// The instance's own value wins either way.
	f := readIntent(t, into)
	f.Settings.Marker = instance.On()
	if err := f.Save(into); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	present(t, "settings.marker on over a manifest that says off", into)

	h.editManifest(t, func(m map[string]any) { delete(m, "marker") })
	f.Settings.Marker = instance.Off()
	if err := f.Save(into); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	missing(t, "settings.marker off under a manifest that says on", into)
	h.mustRun(t, "sync", h.dir, "--into", into)
	missing(t, "settings.marker off, second sync", into)
	h.mustRun(t, "build")
	present(t, "the build directory has no instance file, so the manifest decides", filepath.Join(h.dir, "build", "client"))

	// Rollback builds too, and only an in-place project keeps history to roll back to.
	ip := newInPlace(t)
	ip.editManifest(t, func(m map[string]any) { m["marker"] = false })
	ip.mustRun(t, "add", "sodium")
	ip.mustRun(t, "install")
	ip.mustRun(t, "rollback")
	missing(t, "rollback", ip.dir)
}

func TestMarkerOffLeavesShulkerOutOfExports(t *testing.T) {
	h := newCurseForgeExport(t)
	h.editManifest(t, func(m map[string]any) { m["marker"] = false })

	h.mustRun(t, "export", "mrpack", "--bundle", "--output", filepath.Join(h.dir, "out", "pack.mrpack"))
	h.mustRun(t, "export", "curseforge", "--output", filepath.Join(h.dir, "out", "pack.zip"))
	for _, name := range []string{"pack.mrpack", "pack.zip"} {
		for entry := range readArchive(t, filepath.Join(h.dir, "out", name)) {
			if entry == "shulker.json" || entry == "shulker.lock" || strings.HasPrefix(entry, "profileImage/") || strings.HasSuffix(entry, "shulker-pack.jar") {
				t.Errorf("%s carries %s with the marker off", name, entry)
			}
		}
	}
	var pack curseForgePack
	if err := json.Unmarshal([]byte(readArchive(t, filepath.Join(h.dir, "out", "pack.zip"))["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	if pack.Image != "" {
		t.Fatalf("the profile names an image with the marker off: %q", pack.Image)
	}
}
