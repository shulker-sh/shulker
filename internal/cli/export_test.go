package cli

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/out"
)

type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary"`
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
		if f.Name == build.MrpackIndexFile {
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
	saved := build.MrpackHosts
	build.MrpackHosts = append([]string{u.Hostname()}, saved...)
	t.Cleanup(func() { build.MrpackHosts = saved })
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
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["version"] = "1.0"
		m["note"] = "A demo pack"
		m["targets"] = map[string]any{
			"client": map[string]any{"side": "client", "name": "Demo Pack", "overrides": []string{"overrides/common", "overrides/client"}, "build": "build/client"},
			"server": map[string]any{"side": "server", "overrides": []string{"overrides/common", "overrides/server"}, "build": "build/server"},
		}
		m["server"] = map[string]any{"eula": true, "properties": map[string]any{"motd": "Demo"}}
	})
	writeOverride(t, h.dir, "overrides/common/config/same.txt", "same\n")
	writeOverride(t, h.dir, "overrides/common/config/shared.toml", "common\n")
	writeOverride(t, h.dir, "overrides/client/config/client.toml", "client\n")
	writeOverride(t, h.dir, "overrides/server/config/shared.toml", "server\n")
	h.mustRun(t, "install")

	code, stdout, _ := h.run(t, "export", "mrpack", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "mrpack-host-not-allowed" || len(env.Error.Candidates) != 2 || !strings.HasPrefix(env.Error.Candidates[0], "fabric-api (modrinth, 127.0.0.1") {
		t.Fatalf("export with a foreign host: code=%d env=%+v", code, env)
	}

	h.allowMrpackHost(t)
	stdout = h.mustRun(t, "export", "mrpack")
	archive := filepath.Join(h.dir, "build", "pack-1.0.mrpack")
	if !strings.Contains(stdout, "wrote "+archive+": Demo Pack 1.0, 2 mod(s) by download, 0 bundled, 8 override file(s)") {
		t.Fatalf("export output: %s", stdout)
	}
	index, entries := readMrpack(t, archive)
	if index.FormatVersion != 1 || index.Game != "minecraft" || index.VersionID != "1.0" || index.Name != "Demo Pack" || index.Summary != "A demo pack" {
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
		"server-overrides/eula.txt":           "eula=true\n",
	}
	for path, content := range want {
		if entries[path] != content {
			t.Fatalf("entry %s = %q, want %q (entries: %v)", path, entries[path], content, keys(entries))
		}
	}
	if !strings.Contains(entries["server-overrides/server.properties"], "motd=Demo") || !strings.Contains(entries["client-overrides/options.txt"], "tutorialStep:") {
		t.Fatalf("first-class files: %v", keys(entries))
	}
	if _, ok := entries["client-overrides/mods/shulker-pack.jar"]; !ok || len(entries) != 8 {
		t.Fatalf("entries: %v", keys(entries))
	}
	for name := range entries {
		if strings.Contains(name, "fabric-server-launch") || strings.Contains(name, ".shulker-state") {
			t.Fatalf("unexpected entry %s", name)
		}
	}

	stdout = h.mustRun(t, "export", "mrpack", "--target", "server", "--version", "2.0", "--output", filepath.Join(h.dir, "out", "server.mrpack"), "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	rep := env.Data.(map[string]any)
	if rep["name"] != "pack" || rep["versionId"] != "2.0" || len(rep["mods"].([]any)) != 1 {
		t.Fatalf("server export report: %v", rep)
	}
	index, entries = readMrpack(t, filepath.Join(h.dir, "out", "server.mrpack"))
	if index.Files[0].Path != "mods/"+h.jars["fabric-api"].filename || entries["overrides/config/shared.toml"] != "server\n" || entries["overrides/eula.txt"] == "" || len(entries) != 4 {
		t.Fatalf("server export: files=%+v entries=%v", index.Files, keys(entries))
	}
}

func TestExportMrpackBundlesForeignHosts(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
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
	if entries["overrides/mods/"+h.jars["sodium"].filename] != string(h.jars["sodium"].data) || entries["overrides/mods/"+h.jars["fabric-api"].filename] != string(h.jars["fabric-api"].data) {
		t.Fatalf("bundled entries: %v", keys(entries))
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
