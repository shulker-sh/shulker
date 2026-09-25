package cli

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
)

type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []mrpackIndexFile `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

type mrpackIndexFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

func readMrpack(t *testing.T, path string) (mrpackIndex, map[string]string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var index mrpackIndex
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
		if f.Name == "modrinth.index.json" {
			if err := json.Unmarshal(data, &index); err != nil {
				t.Fatal(err)
			}
			continue
		}
		entries[f.Name] = string(data)
	}
	return index, entries
}

func (h *harness) allowMrpackHost(t *testing.T) {
	t.Helper()
	u, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	saved := packarchive.MrpackHosts
	packarchive.MrpackHosts = append([]string{u.Hostname()}, saved...)
	t.Cleanup(func() { packarchive.MrpackHosts = saved })
}

func writeOverride(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportMrpack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["note"] = "A demo pack"
		m["description"] = "Survival with friends."
		m["client"].(map[string]any)["name"] = "Demo Pack"
		m["server"] = map[string]any{"properties": map[string]any{"motd": "Demo"}}
	})
	h.mustRun(t, "config", "set", "eula", "true")
	writeOverride(t, h.dir, "overrides/config/same.txt", "same\n")
	writeOverride(t, h.dir, "overrides/config/shared.toml", "common\n")
	writeOverride(t, h.dir, "client-overrides/config/client.toml", "client\n")
	writeOverride(t, h.dir, "server-overrides/config/shared.toml", "server\n")
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "export", "mrpack", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "mrpack-host-not-allowed" || len(env.Error.Items) != 2 || !strings.HasPrefix(env.Error.Items[0], "fabric-api (modrinth, 127.0.0.1") {
		t.Fatalf("export with a foreign host: code=%d env=%+v", code, env)
	}

	h.allowMrpackHost(t)
	stdout = h.mustRun(t, "export", "mrpack")
	archive := filepath.Join(h.dir, "build", "pack-1.0.mrpack")
	if !strings.Contains(stdout, "wrote Demo Pack 1.0 » "+archive) || !strings.Contains(stdout, "2 mods by download") || !strings.Contains(stdout, "7 override files") {
		t.Fatalf("export output: %s", stdout)
	}
	index, entries := readMrpack(t, archive)
	if index.FormatVersion != 1 || index.Game != "minecraft" || index.VersionID != "1.0" || index.Name != "Demo Pack" || index.Summary != "Survival with friends.\n\nA demo pack" {
		t.Fatalf("index header: %+v", index)
	}
	if index.Dependencies["minecraft"] != "26.2" || index.Dependencies["fabric-loader"] != "0.17.3" || len(index.Dependencies) != 2 {
		t.Fatalf("dependencies: %v", index.Dependencies)
	}
	if len(index.Files) != 2 {
		t.Fatalf("files: %+v", index.Files)
	}
	api, sodium := index.Files[0], index.Files[1]
	if api.Path != "mods/"+h.jars["fabric-api"].filename || api.Hashes["sha512"] != h.jars["fabric-api"].sha512 || api.Hashes["sha1"] != h.jars["fabric-api"].sha1 || api.FileSize != int64(len(h.jars["fabric-api"].data)) || api.Env["client"] != "required" || api.Env["server"] != "required" || api.Downloads[0] != h.server.URL+"/cdn/"+h.jars["fabric-api"].filename {
		t.Fatalf("fabric-api entry: %+v", api)
	}
	if sodium.Path != "mods/"+h.jars["sodium"].filename || sodium.Env["client"] != "required" || sodium.Env["server"] != "unsupported" {
		t.Fatalf("sodium entry: %+v", sodium)
	}
	want := map[string]string{
		"overrides/config/same.txt":           "same\n",
		"client-overrides/config/shared.toml": "common\n",
		"server-overrides/config/shared.toml": "server\n",
		"client-overrides/config/client.toml": "client\n",
	}
	for path, content := range want {
		if entries[path] != content {
			t.Fatalf("entry %s = %q, want %q (entries: %v)", path, entries[path], content, keys(entries))
		}
	}
	if !strings.Contains(entries["server-overrides/server.properties"], "motd=Demo") || !strings.Contains(entries["client-overrides/options.txt"], "tutorialStep:") {
		t.Fatalf("first-class files: %v", keys(entries))
	}
	if _, ok := entries["client-overrides/mods/shulker-pack.jar"]; !ok || len(entries) != 10 {
		t.Fatalf("entries: %v", keys(entries))
	}
	if !strings.Contains(entries["shulker.json"], `"sodium"`) || !strings.Contains(entries["shulker.lock"], `"sodium"`) {
		t.Fatalf("archive identity: %v", keys(entries))
	}
	for name := range entries {
		if strings.Contains(name, "fabric-server-launch") || strings.Contains(name, ".shulker/") {
			t.Fatalf("unexpected entry %s", name)
		}
	}

	stdout = h.mustRun(t, "export", "mrpack", "--side", "server", "--version", "2.0", "--output", filepath.Join(h.dir, "out", "server.mrpack"), "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	rep := env.Data.(map[string]any)
	if rep["name"] != "pack" || rep["version"] != "2.0" || len(rep["mods"].([]any)) != 1 || fmt.Sprint(rep["sides"]) != "[server]" {
		t.Fatalf("server export report: %v", rep)
	}
	if stdout := h.mustRun(t, "export", "mrpack", "--side", "server", "--version", "2.0", "--output", filepath.Join(h.dir, "out", "server.mrpack")); !strings.Contains(stdout, "(server)") {
		t.Fatalf("a partial export names its side: %s", stdout)
	}
	index, entries = readMrpack(t, filepath.Join(h.dir, "out", "server.mrpack"))
	if index.Files[0].Path != "mods/"+h.jars["fabric-api"].filename || entries["overrides/config/shared.toml"] != "server\n" || len(entries) != 6 {
		t.Fatalf("server export: files=%+v entries=%v", index.Files, keys(entries))
	}
}

func TestExportMrpackCarriesPacks(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fresh-animations")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	h.mustRun(t, "install")
	h.allowMrpackHost(t)

	stdout := h.mustRun(t, "export", "mrpack", "--version", "1.0")
	for _, want := range []string{"1 resource pack by download", "1 shader by download"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("export output has no %q: %s", want, stdout)
		}
	}
	index, entries := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if len(index.Files) != 2 {
		t.Fatalf("files: %+v", index.Files)
	}
	// Each pack goes in under its requires key, and both are client-only.
	pack, shader := index.Files[0], index.Files[1]
	if pack.Path != "resourcepacks/FreshAnimations_v1.9.4.zip" || pack.Env["client"] != "required" || pack.Env["server"] != "unsupported" {
		t.Fatalf("resource pack entry: %+v", pack)
	}
	if shader.Path != "shaderpacks/ComplementaryReimagined_r5.5.1.zip" || shader.Env["server"] != "unsupported" {
		t.Fatalf("shader entry: %+v", shader)
	}
	if !strings.Contains(entries["shulker.json"], "complementary-reimagined") || !strings.Contains(entries["shulker.lock"], "fresh-animations") {
		t.Fatalf("archive identity: %v", keys(entries))
	}
}

func TestExportMrpackBundlesForeignHosts(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	code, _, _ := h.run(t, "export", "mrpack")
	if code == 0 {
		t.Fatal("export without a version should fail")
	}
	_, stderr := h.mustRunStderr(t, "export", "mrpack", "--version", "0.1", "--bundle")
	if !strings.Contains(stderr, "bundled fabric-api from modrinth, 127.0.0.1") || !strings.Contains(stderr, "bundled sodium") {
		t.Fatalf("bundle warnings: %s", stderr)
	}
	index, entries := readMrpack(t, filepath.Join(h.dir, "build", "pack-0.1.mrpack"))
	if len(index.Files) != 0 {
		t.Fatalf("files: %+v", index.Files)
	}
	if entries["client-overrides/mods/"+h.jars["sodium"].filename] != string(h.jars["sodium"].data) || entries["overrides/mods/"+h.jars["fabric-api"].filename] != string(h.jars["fabric-api"].data) {
		t.Fatalf("bundled entries: %v", keys(entries))
	}

	// A bundled file is written into an override folder, but it is already counted as
	// bundled, so the override tally must not count it a second time.
	_, stdout, _ := h.run(t, "--json", "export", "mrpack", "--version", "0.1", "--bundle")
	var report struct {
		Data struct {
			Overrides   []string `json:"overrides"`
			BundledMods []string `json:"bundledMods"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Data.BundledMods) != 2 {
		t.Fatalf("both mods should be bundled: %+v", report.Data)
	}
	for _, path := range report.Data.Overrides {
		if strings.HasSuffix(path, h.jars["sodium"].filename) || strings.HasSuffix(path, h.jars["fabric-api"].filename) {
			t.Fatalf("a bundled file must not be counted as an override too: %v", report.Data.Overrides)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExportMrpackBundleRoundTripsASidedLocalJar(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	jar := makeJar(t, "private-mod", "private-mod-1.4.jar", "client")
	h.mustRun(t, "add", writeOutside(t, jar.filename, jar.data))
	h.editManifest(t, func(m map[string]any) { m["version"] = "1.0" })
	h.mustRun(t, "install")

	h.mustRun(t, "export", "mrpack", "--bundle")
	archive := filepath.Join(h.dir, "build", "pack-1.0.mrpack")
	_, entries := readMrpack(t, archive)
	if entries["client-overrides/mods/private-mod-1.4.jar"] != string(jar.data) {
		t.Fatalf("a client-only local jar goes in client-overrides/: %v", keys(entries))
	}

	dir := filepath.Join(t.TempDir(), "again")
	code, stdout, stderr := h.run(t, "import", archive, "--dir", dir)
	if code != 0 || strings.Contains(stdout+stderr, "side") {
		t.Fatalf("import: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	m, l := readProject(t, dir)
	if m.Requires["private-mod"].File != "files/private-mod-1.4.jar" || l.Mods["private-mod"].Side != "client" {
		t.Fatalf("round trip: %+v %+v", m.Requires["private-mod"], l.Mods["private-mod"])
	}
}
