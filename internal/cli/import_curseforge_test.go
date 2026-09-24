package cli

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// cfManifest is a CurseForge pack's manifest.json, as the format writes it.
type cfManifest struct {
	Minecraft       cfMinecraft  `json:"minecraft"`
	ManifestType    string       `json:"manifestType"`
	ManifestVersion int          `json:"manifestVersion"`
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Author          string       `json:"author,omitempty"`
	Files           []cfPackFile `json:"files"`
	Overrides       string       `json:"overrides"`
	Image           string       `json:"image,omitempty"`
}

type cfMinecraft struct {
	Version    string        `json:"version"`
	ModLoaders []cfModLoader `json:"modLoaders"`
}

type cfModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

type cfPackFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

func writeCurseForgeZip(t *testing.T, path string, m cfManifest, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	entries["manifest.json"] = data
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

var craftFiles = []cfPackFile{
	{ProjectID: 238222, FileID: 5000001, Required: true},
	{ProjectID: 306612, FileID: 5000010, Required: true},
	{ProjectID: 600000, FileID: 5300001, Required: true},
}

func importedCurseForgePack(files ...cfPackFile) cfManifest {
	return cfManifest{
		Minecraft:    cfMinecraft{Version: "26.2", ModLoaders: []cfModLoader{{ID: "fabric-0.17.3", Primary: true}}},
		ManifestType: "minecraftModpack", ManifestVersion: 1,
		Name: "Craft Pack", Version: "3.1", Author: "someone", Files: files, Overrides: "extras",
	}
}

func TestImportRefusesAnArchiveThatIsNoModpack(t *testing.T) {
	h := newHarness(t)
	h.dir = t.TempDir()
	resourcePack := filepath.Join(t.TempDir(), "pack.zip")
	f, _ := os.Create(resourcePack)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("pack.mcmeta")
	w.Write([]byte("{}"))
	zw.Close()
	f.Close()
	code, stdout, _ := h.run(t, "--json", "import", resourcePack)
	if e := failureCode(t, stdout); code == 0 || e.Code != "archive-not-modpack" {
		t.Fatalf("expected archive-not-modpack, got %d %s", code, stdout)
	}
}
