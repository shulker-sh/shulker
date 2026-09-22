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
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
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
		if f.ProjectID != want[i][0] || f.FileID != want[i][1] || !f.Required {
			t.Fatalf("file %d: %+v", i, f)
		}
	}
	if !strings.Contains(entries["modlist.html"], `<li><a href="https://www.curseforge.com/projects/238222">jei</a></li>`) {
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

func TestExportCurseForgeUnmatchedMods(t *testing.T) {
	h := newCurseForgeExport(t)
	delete(h.cfMods, 394468)

	code, stdout, _ := h.run(t, "export", "curseforge", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "curseforge-not-found" || len(e.Items) != 1 || !strings.HasPrefix(e.Items[0], "sodium (modrinth, 127.0.0.1") {
		t.Fatalf("unmatched mod: code=%d %+v", code, e)
	}
	_, _, stderr := h.run(t, "export", "curseforge")
	if !strings.Contains(stderr, "1 mod isn't on CurseForge") || !strings.Contains(stderr, "sodium (modrinth, 127.0.0.1") || !strings.Contains(stderr, "shulker export curseforge --bundle") {
		t.Fatalf("unmatched mod output: %s", stderr)
	}

	stdout, stderr = h.mustRunStderr(t, "export", "curseforge", "--bundle")
	if !strings.Contains(stderr, "bundled sodium from modrinth") || !strings.Contains(stdout, "2 mods by file ID") || !strings.Contains(stdout, "1 mod bundled") || !strings.Contains(stdout, "matched on CurseForge: fabric-api") || strings.Contains(stdout, "fabric-api, sodium") {
		t.Fatalf("bundle output: stdout=%s stderr=%s", stdout, stderr)
	}
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	if entries["overrides/mods/"+h.jars["sodium"].filename] != string(h.jars["sodium"].data) {
		t.Fatalf("bundled entries: %v", keys(entries))
	}
}

func TestExportCurseForgeWithoutKey(t *testing.T) {
	h := newCurseForgeExport(t)
	h.noCurseForge = true

	code, stdout, _ := h.run(t, "export", "curseforge", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "provider-unavailable" || strings.Join(e.Items, ",") != "fabric-api,sodium" {
		t.Fatalf("no key: code=%d %+v", code, e)
	}
	_, stderr := h.mustRunStderr(t, "export", "curseforge", "--bundle")
	if !strings.Contains(stderr, "CurseForge lookup failed, so these are bundled: curseforge needs an API key") || !strings.Contains(stderr, "bundled fabric-api") || !strings.Contains(stderr, "bundled sodium") {
		t.Fatalf("no key with --bundle: %s", stderr)
	}
}

func TestExportCurseForgeLockedModsNeedNoLookup(t *testing.T) {
	h := newCurseForgeExport(t)
	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "add", "fabric-api", "--provider", "curseforge")
	if api := h.readLock(t).Mods["fabric-api"]; api.Provider != "curseforge" {
		t.Fatalf("fabric-api should be locked from CurseForge: %+v", api)
	}
	h.mustRun(t, "install")
	h.noCurseForge = true

	stdout, stderr := h.mustRunStderr(t, "export", "curseforge")
	if !strings.Contains(stdout, "2 mods by file ID") || strings.Contains(stdout, "matched on CurseForge") || strings.Contains(stderr, "looked up") {
		t.Fatalf("export of CurseForge-locked mods: stdout=%s stderr=%s", stdout, stderr)
	}
}

func TestExportCurseForgeCarriesPacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "jei", "--provider", "curseforge")
	// No --type: the class the slug matches in settles what it is.
	h.mustRun(t, "add", "fresh-animations", "--provider", "curseforge")
	h.mustRun(t, "install")

	stdout := h.mustRun(t, "export", "curseforge", "--version", "1.0")
	for _, want := range []string{"2 mods by file ID", "1 resource pack by file ID"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("export output has no %q: %s", want, stdout)
		}
	}
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	var pack curseForgePack
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	want := [][2]int{{306612, 5000010}, {238222, 5000001}, {600000, 5300001}}
	if len(pack.Files) != len(want) {
		t.Fatalf("files: %+v", pack.Files)
	}
	for i, f := range pack.Files {
		if f.ProjectID != want[i][0] || f.FileID != want[i][1] || !f.Required {
			t.Fatalf("file %d: %+v", i, f)
		}
	}
	for name := range entries {
		if strings.HasPrefix(name, "overrides/resourcepacks/") {
			t.Fatalf("a pack locked from CurseForge was bundled: %s", name)
		}
	}
}

func TestExportCurseForgeEnablesPacksByTheirCurseForgeNames(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations", "--provider", "curseforge")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "install")

	h.mustRun(t, "export", "curseforge", "--version", "1.0")
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	if options := entries["overrides/options.txt"]; !strings.Contains(options, `"file/FreshAnimations_CF_v1.9.4.zip"`) || strings.Contains(options, "fresh-animations.zip") {
		t.Fatalf("options.txt: %s", options)
	}
	if iris := entries["overrides/config/iris.properties"]; !strings.Contains(iris, "shaderPack=ComplementaryReimagined_r5.5.1.zip") {
		t.Fatalf("iris.properties: %s", iris)
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
