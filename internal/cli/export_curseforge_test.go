package cli

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/provider/curseforge"
)

type curseForgePack struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	ManifestType    string `json:"manifestType"`
	ManifestVersion int    `json:"manifestVersion"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Author          string `json:"author"`
	Files           []struct {
		ProjectID int   `json:"projectID"`
		FileID    int   `json:"fileID"`
		Required  bool  `json:"required"`
		IsLocked  *bool `json:"isLocked"`
	} `json:"files"`
	Overrides string `json:"overrides"`
	Image     string `json:"image"`
}

func (h *harness) fingerprintMatches(fingerprints []uint32) []map[string]any {
	matches := []map[string]any{}
	for _, m := range h.cfMods {
		for _, f := range m.files {
			fp := curseforge.Fingerprint(f.jar.data)
			for _, want := range fingerprints {
				if fp == want {
					matches = append(matches, map[string]any{"id": f.id, "file": map[string]any{"id": f.id, "modId": m.id, "fileName": f.jar.filename, "fileFingerprint": fp}})
				}
			}
		}
	}
	return matches
}

func readArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	entries := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(data)
	}
	return entries
}

func newCurseForgeExport(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "add", "jei", "--provider", "curseforge")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["authors"] = []string{"Ann", "Bo"}
		m["client"].(map[string]any)["name"] = "Demo Pack"
		m["server"] = map[string]any{"eula": true}
	})
	h.mustRun(t, "install")
	return h
}

func TestExportCurseForge(t *testing.T) {
	h := newCurseForgeExport(t)
	stdout := h.mustRun(t, "export", "curseforge")
	archive := filepath.Join(h.dir, "build", "pack-1.0.zip")
	if !strings.Contains(stdout, "wrote Demo Pack 1.0 » "+archive) || !strings.Contains(stdout, "3 mods by file ID") || !strings.Contains(stdout, "matched on CurseForge: fabric-api, sodium") {
		t.Fatalf("export output: %s", stdout)
	}
	entries := readArchive(t, archive)
	var pack curseForgePack
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	if pack.ManifestType != "minecraftModpack" || pack.ManifestVersion != 1 || pack.Name != "Demo Pack" || pack.Version != "1.0" || pack.Author != "Ann, Bo" || pack.Overrides != "overrides" || pack.Image != "profileImage/icon.png" {
		t.Fatalf("manifest header: %+v", pack)
	}
	if image := entries["profileImage/icon.png"]; !strings.HasPrefix(image, "\x89PNG") {
		t.Fatalf("profile image entry: %d bytes", len(image))
	}
	if pack.Minecraft.Version != "26.2" || len(pack.Minecraft.ModLoaders) != 1 || pack.Minecraft.ModLoaders[0].ID != "fabric-0.17.3" || !pack.Minecraft.ModLoaders[0].Primary {
		t.Fatalf("minecraft: %+v", pack.Minecraft)
	}
	want := [][2]int{{306612, 5000010}, {238222, 5000001}, {394468, 5000020}}
	if len(pack.Files) != len(want) {
		t.Fatalf("files: %+v", pack.Files)
	}
	for i, f := range pack.Files {
		if f.ProjectID != want[i][0] || f.FileID != want[i][1] || !f.Required || f.IsLocked == nil || *f.IsLocked {
			t.Fatalf("file %d: %+v", i, f)
		}
	}
	if modlist := entries["modlist.html"]; !strings.HasPrefix(modlist, "<ul>") || !strings.Contains(modlist, `<li><a href="https://www.curseforge.com/minecraft/mc-mods/jei">JEI (by jei-dev)</a></li>`) {
		t.Fatalf("modlist: %s", entries["modlist.html"])
	}
	if _, ok := entries["overrides/options.txt"]; !ok {
		t.Fatalf("entries: %v", keys(entries))
	}
	if !strings.Contains(entries["shulker.json"], `"sodium"`) || !strings.Contains(entries["shulker.lock"], `"sodium"`) {
		t.Fatalf("archive identity: %v", keys(entries))
	}
	for name := range entries {
		if strings.HasPrefix(name, "overrides/mods/") && name != "overrides/mods/shulker-pack.jar" {
			t.Fatalf("a matched mod was bundled: %s", name)
		}
	}
}

func TestExportFromRemoteSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newCurseForgeExport(t)
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	h.mustRun(t, "export", "curseforge", source, "--ref", "main", "--version", "2.0")
	if _, err := os.Stat(filepath.Join(h.dir, "pack-2.0.zip")); err != nil {
		t.Fatalf("a remote export should land in the current directory: %v", err)
	}
	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", source, "--version", "2.0")
	if _, err := os.Stat(filepath.Join(h.dir, "pack-2.0.mrpack")); err != nil {
		t.Fatalf("a remote mrpack export should land in the current directory: %v", err)
	}
}
