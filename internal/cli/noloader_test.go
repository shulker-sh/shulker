package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
)

func TestInitWithoutALoader(t *testing.T) {
	h := newHarness(t)
	stdout := h.mustRun(t, "init", "--yes", "--name", "pack")
	if !strings.Contains(stdout, "created shulker.json (Minecraft 26.2, Java 25)") || !strings.Contains(stdout, "Add a resource pack or shader") || !strings.Contains(stdout, "$ shulker add <name>") {
		t.Fatalf("init output: %s", stdout)
	}
	for _, file := range []string{"shulker.json", "shulker.lock"} {
		var raw map[string]any
		h.readJSON(t, file, &raw)
		if _, has := raw["loader"]; has {
			t.Fatalf("%s names a loader: %v", file, raw["loader"])
		}
	}

	server := newHarness(t)
	stdout = server.mustRun(t, "init", "--yes", "--name", "pack", "--side", "server")
	if !strings.Contains(stdout, "Download and build it") || !strings.Contains(stdout, "$ shulker install") {
		t.Fatalf("server init output: %s", stdout)
	}

	fresh := newHarness(t)
	if code, _, _ := fresh.run(t, "init", "--yes", "--loader-version", "0.17.3"); code != out.ExitUsage {
		t.Fatalf("--loader-version without --loader: code=%d", code)
	}
	if code, _, _ := fresh.run(t, "init", "--yes", "--loader", "none", "--json"); code != 0 {
		t.Fatalf("--loader none: code=%d", code)
	}
}

func TestAddNeedsALoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	code, stdout, _ := h.run(t, "add", "sodium", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "loader-required" || !strings.Contains(e.Message, "shulker set loader.type <fabric|quilt|neoforge|forge>") {
		t.Fatalf("add without a loader: code=%d %s", code, stdout)
	}
	h.mustRun(t, "set", "loader.type", "fabric")
	h.mustRun(t, "add", "sodium")
	m, l := readProject(t, h.dir)
	if m.Loader.Version != "" || l.Loader.Type != "fabric" || l.Loader.Version != "0.17.3" || len(m.Mods()) != 1 {
		t.Fatalf("after set loader.type: manifest %+v, lock %+v", m.Loader, l.Loader)
	}
}

func TestServerWithoutALoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")
	_, l := readProject(t, h.dir)
	if l.Server == nil || l.Server.Sha512 != h.vanilla.sha512 || l.Loader.Type != "" {
		t.Fatalf("lock: server %+v, loader %+v", l.Server, l.Loader)
	}
	buildDir := filepath.Join(h.dir, "build", "server")
	if got := readFile(t, filepath.Join(buildDir, build.VanillaServerFile)); got != string(h.vanilla.data) {
		t.Fatal("server.jar is not the vanilla jar")
	}
	if _, err := os.Stat(filepath.Join(buildDir, ".fabric")); err == nil {
		t.Fatal("a project without a loader placed Fabric's data dir")
	}
	if got := build.LaunchArgs(l); !slices.Equal(got, []string{"-jar", "server.jar"}) {
		t.Fatalf("launch args: %v", got)
	}
}

func TestClientWithoutALoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods")); err == nil {
		t.Fatal("a project without a loader has a mods dir with the marker jar")
	}

	mrpackPath := filepath.Join(t.TempDir(), "pack.mrpack")
	h.mustRun(t, "export", "mrpack", "--version", "1.0", "-o", mrpackPath)
	index, entries := readMrpack(t, mrpackPath)
	if len(index.Dependencies) != 1 || index.Dependencies["minecraft"] != "26.2" {
		t.Fatalf("dependencies: %v", index.Dependencies)
	}
	for name := range entries {
		if strings.HasPrefix(name, "overrides/mods/") {
			t.Fatalf("exported a mod: %s", name)
		}
	}

	zipPath := filepath.Join(t.TempDir(), "pack.zip")
	h.mustRun(t, "export", "curseforge", "--version", "1.0", "-o", zipPath)
	var pack struct {
		Minecraft struct {
			ModLoaders json.RawMessage `json:"modLoaders"`
		} `json:"minecraft"`
	}
	if err := json.Unmarshal([]byte(readArchive(t, zipPath)["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	if string(pack.Minecraft.ModLoaders) != "[]" {
		t.Fatalf("modLoaders: %s", pack.Minecraft.ModLoaders)
	}
}

func TestLinkWithoutALoader(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")

	mojangDir := t.TempDir()
	stdout := h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	if strings.Contains(stdout, "installed") || !strings.Contains(stdout, "linked launcher profile pack") {
		t.Fatalf("link mojang output: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(mojangDir, "versions")); err == nil {
		t.Fatal("a profile without a loader installed a version")
	}
	var profile map[string]any
	for _, p := range readProfiles(t, mojangDir).Profiles {
		if p["name"] == "pack" {
			profile = p
		}
	}
	if profile["lastVersionId"] != "26.2" {
		t.Fatalf("profile: %v", profile)
	}

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir)
	var pack struct {
		Components []map[string]any `json:"components"`
	}
	readJSONFile(t, filepath.Join(prismDir, "instances", "shulker-pack", launcher.PackFile), &pack)
	if len(pack.Components) != 1 || pack.Components[0]["uid"] != "net.minecraft" || pack.Components[0]["version"] != "26.2" {
		t.Fatalf("mmc-pack.json: %+v", pack)
	}

	atlDir := t.TempDir()
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", atlDir)
	inst := readATLInstance(t, filepath.Join(atlDir, "instances", "pack"))
	if _, has := inst["launcher"].(map[string]any)["loaderVersion"]; has || inst["id"] != "26.2" {
		t.Fatalf("ATLauncher instance.json: %v", inst)
	}

	gdlDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", gdlDir)
	version := readGDLInstance(t, filepath.Join(gdlDir, "instances", "pack"))["game_configuration"].(map[string]any)["version"].(map[string]any)
	if loaders := version["modloaders"].([]any); len(loaders) != 0 || version["release"] != "26.2" {
		t.Fatalf("GDLauncher version: %v", version)
	}
	if len(h.installs) != 0 {
		t.Fatalf("no loader means no installer: %v", h.installs)
	}
}

func TestImportMrpackWithoutALoader(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(t.TempDir(), "vanilla.mrpack")
	writeMrpack(t, archive, mrpack.Index{
		FormatVersion: 1, Game: "minecraft", VersionID: "1.0", Name: "Vanilla",
		Files:        []mrpack.File{},
		Dependencies: map[string]string{"minecraft": "26.2"},
	}, map[string][]byte{"overrides/options.txt": []byte("fov:0.5\n")})
	dir := filepath.Join(t.TempDir(), "vanilla")
	stdout := h.mustRun(t, "import", "mrpack", archive, "--dir", dir)
	if !strings.Contains(stdout, "(Minecraft 26.2)") {
		t.Fatalf("import output: %s", stdout)
	}
	m, l := readProject(t, dir)
	if m.Loader.Type != "" || l.Loader.Type != "" || l.Minecraft != "26.2" {
		t.Fatalf("manifest loader %+v, lock loader %+v", m.Loader, l.Loader)
	}
}
