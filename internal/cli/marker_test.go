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
)

func TestClientBuildWritesMarkerJar(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my.pack")
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
	if meta.SchemaVersion != 1 || meta.ID != "shulker_my_pack" || meta.Name != "my.pack" || meta.Environment != "*" || len(meta.Version) != 8 || meta.Custom.ModMenu.UpdateChecker {
		t.Fatalf("fabric.mod.json: %+v", meta)
	}
	want := "Minecraft 26.2 \u00b7 fabric 0.17.3 \u00b7 2 mods\n\nMods\n  \u2022 sodium\n\nDependencies\n  \u2022 fabric-api"
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
			t.Fatalf("%s targets class version %d, want 61 (Java 17)", class, major)
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
	if !strings.Contains(stdout, "0 written, 4 unchanged") {
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
	h.mustRun(t, "init", "--yes", "--name", "linked")
	h.editManifest(t, func(m map[string]any) {
		m["description"] = "Survival with friends."
		m["authors"] = []string{"Alice", "shulker.sh"}
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
	if !strings.HasPrefix(meta.Description, "Survival with friends.\n\nMinecraft 26.2 \u00b7 ") {
		t.Fatalf("description:\n%s", meta.Description)
	}
	if strings.Join(meta.Authors, ",") != "Alice,shulker.sh" {
		t.Fatalf("authors: %v", meta.Authors)
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
	h.mustRun(t, "init", "--yes", "--name", "versioned")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["targets"].(map[string]any)["client"].(map[string]any)["name"] = "LAN Party"
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
	h.mustRun(t, "init", "--yes", "--name", "seeded")
	m := h.readManifest(t)
	if len(m.Authors) == 0 || m.Authors[len(m.Authors)-1] != "shulker.sh" {
		t.Fatalf("authors: %v", m.Authors)
	}
}
