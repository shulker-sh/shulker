package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/cache"
	"github.com/andrewmast/shulker/internal/fetch"
	"github.com/andrewmast/shulker/internal/meta"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/provider"
	"github.com/andrewmast/shulker/internal/provider/modrinth"
	"github.com/andrewmast/shulker/internal/resolve"
)

type fakeJar struct {
	id, filename string
	data         []byte
	sha512       string
}

func makeJar(t *testing.T, id, filename string, env string) fakeJar {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `{"id":%q,"version":"1.0.0","environment":%q,"depends":{"fabricloader":">=0.17"}}`, id, env)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(buf.Bytes())
	return fakeJar{id: id, filename: filename, data: buf.Bytes(), sha512: hex.EncodeToString(sum[:])}
}

type harness struct {
	server *httptest.Server
	jars   map[string]fakeJar
	dir    string
	cache  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{dir: t.TempDir(), cache: t.TempDir(), jars: map[string]fakeJar{}}
	sodium := makeJar(t, "sodium", "sodium-fabric-0.9.2+mc26.2.jar", "client")
	fabricAPI := makeJar(t, "fabric-api", "fabric-api-0.130.0+26.2.jar", "*")
	h.jars["sodium"], h.jars["fabric-api"] = sodium, fabricAPI

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/piston/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"latest": map[string]string{"release": "26.2", "snapshot": "26.3-pre-1"},
			"versions": []map[string]string{
				{"id": "26.3-pre-1", "type": "snapshot", "url": base + "/piston/26.3-pre-1.json"},
				{"id": "26.2", "type": "release", "url": base + "/piston/26.2.json"},
				{"id": "26.1", "type": "release", "url": base + "/piston/26.1.json"},
			},
		})
	})
	mux.HandleFunc("/piston/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"javaVersion": map[string]any{"component": "java-runtime-epsilon", "majorVersion": 25}})
	})
	mux.HandleFunc("/fabric/versions/loader/26.2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"loader": map[string]any{"version": "0.18.0-beta.1", "stable": false}},
			{"loader": map[string]any{"version": "0.17.3", "stable": true}},
			{"loader": map[string]any{"version": "0.17.2", "stable": true}},
		})
	})
	projects := map[string]map[string]any{
		"sodium":     {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"AANobbMI":   {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"fabric-api": {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
		"P7dR8mSH":   {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
	}
	versionOf := func(projectID string, jar fakeJar, deps []map[string]any) map[string]any {
		return map[string]any{
			"id": "Q" + projectID[1:], "project_id": projectID, "version_number": "1.0.0+mc26.2", "version_type": "release",
			"date_published": "2026-09-01T00:00:00Z", "game_versions": []string{"26.2"}, "loaders": []string{"fabric"},
			"files":        []map[string]any{{"url": base + "/cdn/" + jar.filename, "filename": jar.filename, "primary": true, "hashes": map[string]string{"sha512": jar.sha512}}},
			"dependencies": deps,
		}
	}
	mux.HandleFunc("/modrinth/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/modrinth/project/")
		if strings.HasSuffix(rest, "/version") {
			switch strings.TrimSuffix(rest, "/version") {
			case "AANobbMI":
				writeJSON(w, []map[string]any{versionOf("AANobbMI", sodium, []map[string]any{{"project_id": "P7dR8mSH", "dependency_type": "required"}})})
			case "P7dR8mSH":
				writeJSON(w, []map[string]any{versionOf("P7dR8mSH", fabricAPI, nil)})
			default:
				http.NotFound(w, r)
			}
			return
		}
		p, ok := projects[rest]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, p)
	})
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, r *http.Request) {
		for _, jar := range h.jars {
			if strings.HasSuffix(r.URL.Path, jar.filename) {
				w.Write(jar.data)
				return
			}
		}
		http.NotFound(w, r)
	})
	h.server = httptest.NewServer(mux)
	base = h.server.URL
	t.Cleanup(h.server.Close)
	return h
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *harness) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := newApp(&stdout, &stderr)
	a.dir = h.dir
	f := fetch.New("test")
	piston := meta.NewPiston(f)
	piston.ManifestURL = h.server.URL + "/piston/manifest.json"
	fabric := meta.NewFabric(f)
	fabric.BaseURL = h.server.URL + "/fabric"
	mr := modrinth.New(f)
	mr.BaseURL = h.server.URL + "/modrinth"
	a.d = &deps{
		fetch:     f,
		cache:     &cache.Cache{Dir: h.cache},
		providers: map[string]provider.Provider{"modrinth": mr},
		meta:      &resolve.Meta{Piston: piston, Fabric: fabric},
	}
	code := a.run(args)
	return code, stdout.String(), stderr.String()
}

func (h *harness) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := h.run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout
}

func (h *harness) readJSON(t *testing.T, rel string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
}

func TestVerticalSlice(t *testing.T) {
	h := newHarness(t)

	stdout := h.mustRun(t, "init", "--yes")
	if !strings.Contains(stdout, "Minecraft 26.2 with fabric 0.17.3 (Java 25)") {
		t.Fatalf("init output: %s", stdout)
	}
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	if m["minecraft"] != "26.2" || m["name"] != filepath.Base(h.dir) {
		t.Fatalf("manifest: %v", m)
	}
	if code, _, _ := h.run(t, "init", "--yes"); code == 0 {
		t.Fatal("second init should fail")
	}

	stdout = h.mustRun(t, "add", "sodium", "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.LockStale {
		t.Fatalf("add envelope: %+v", env)
	}
	added := env.Data.([]any)[0].(map[string]any)
	if added["id"] != "sodium" || added["side"] != "client" || added["dependencies"].([]any)[0] != "fabric-api" {
		t.Fatalf("added: %v", added)
	}
	var l struct {
		Mods map[string]struct {
			Side       string   `json:"side"`
			RequiredBy []string `json:"requiredBy"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if l.Mods["fabric-api"].Side != "both" || l.Mods["fabric-api"].RequiredBy[0] != "sodium" || len(l.Mods["sodium"].RequiredBy) != 0 {
		t.Fatalf("lock mods: %+v", l.Mods)
	}
	h.readJSON(t, "shulker.json", &m)
	if mods := m["mods"].(map[string]any); len(mods) != 1 || len(mods["sodium"].(map[string]any)) != 0 {
		t.Fatalf("manifest mods: %v", m["mods"])
	}

	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "options.txt.tmpl"), []byte("renderDistance:12\nlang:${lang}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := h.run(t, "install"); code == 0 || !strings.Contains(stderr, "${lang}") {
		t.Fatalf("unset variable should fail: %d %s", code, stderr)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "options.txt.tmpl"), []byte("renderDistance:12\nlang:en_us\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "install")
	if !strings.Contains(stdout, "fetched 0 file(s)") || !strings.Contains(stdout, "client: 3 written") {
		t.Fatalf("install output: %s", stdout)
	}
	buildDir := filepath.Join(h.dir, "build", "client")
	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "mods/" + h.jars["fabric-api"].filename, "options.txt", build.StateFile} {
		if _, err := os.Stat(filepath.Join(buildDir, rel)); err != nil {
			t.Fatal(err)
		}
	}
	var state build.State
	h.readJSON(t, "build/client/.shulker-state.json", &state)
	if state.Target != "client" || len(state.Files) != 3 {
		t.Fatalf("state: %+v", state)
	}

	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "0 written, 3 unchanged") {
		t.Fatalf("rebuild output: %s", stdout)
	}

	options := filepath.Join(buildDir, "options.txt")
	if err := os.WriteFile(options, []byte("renderDistance:8\nlang:en_us\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 kept") || !strings.Contains(stdout, "kept options.txt") {
		t.Fatalf("edited build file should be kept: %s", stdout)
	}
	if data, _ := os.ReadFile(options); !strings.Contains(string(data), "renderDistance:8") {
		t.Fatal("kept file was overwritten")
	}

	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "options.txt.tmpl"), []byte("renderDistance:16\nlang:en_us\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "build", "--json")
	if code == 0 {
		t.Fatal("both-sides change should conflict")
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil || env.Error.Code != "build-conflict" || len(env.Error.Candidates) != 1 {
		t.Fatalf("conflict envelope: %+v", env)
	}
	h.mustRun(t, "build", "--force")
	if data, _ := os.ReadFile(options); !strings.Contains(string(data), "renderDistance:16") {
		t.Fatal("force should overwrite")
	}

	if err := os.Remove(filepath.Join(h.dir, "overrides", "options.txt.tmpl")); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 removed") {
		t.Fatalf("removed source should delete build file: %s", stdout)
	}
	if _, err := os.Stat(options); !os.IsNotExist(err) {
		t.Fatal("build file should be gone")
	}

	foreign := filepath.Join(buildDir, "mods", "handmade.jar")
	if err := os.WriteFile(foreign, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("build must never delete a file it did not write")
	}
}

func TestAddUnknownMod(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	code, stdout, _ := h.run(t, "add", "nope", "--json")
	if code == 0 {
		t.Fatal("expected failure")
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "nope") {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestStaleLockBlocksBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	path := filepath.Join(h.dir, "shulker.json")
	data, _ := os.ReadFile(path)
	data = bytes.Replace(data, []byte(`"sodium": {}`), []byte(`"sodium": {"channel": "beta"}`), 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || !env.LockStale || env.Error.Code != "lock-stale" {
		t.Fatalf("code=%d env=%+v", code, env)
	}
}

func TestRemovePrunesOrphans(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "remove", "fabric-api", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "not-direct" || env.Error.Candidates[0] != "sodium" {
		t.Fatalf("removing a dependency: code=%d env=%+v", code, env)
	}
	if code, stdout, _ = h.run(t, "remove", "nope", "--json"); code == 0 || !strings.Contains(stdout, `"not-found"`) {
		t.Fatalf("removing an unknown mod: code=%d %s", code, stdout)
	}

	stdout = h.mustRun(t, "remove", "sodium", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data := env.Data.(map[string]any)
	if !env.OK || env.LockStale || data["removed"].([]any)[0] != "sodium" || data["pruned"].([]any)[0] != "fabric-api" {
		t.Fatalf("remove envelope: %+v", env)
	}
	var l struct {
		Mods map[string]any `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	var m struct {
		Mods map[string]any `json:"mods"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(l.Mods) != 0 || len(m.Mods) != 0 {
		t.Fatalf("lock mods %v, manifest mods %v", l.Mods, m.Mods)
	}

	h.mustRun(t, "add", "sodium", "fabric-api")
	stdout = h.mustRun(t, "remove", "sodium")
	if strings.Contains(stdout, "pruned") {
		t.Fatalf("direct fabric-api must survive: %s", stdout)
	}
	var kept struct {
		Mods map[string]struct {
			RequiredBy []string `json:"requiredBy"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &kept)
	if fa, ok := kept.Mods["fabric-api"]; !ok || len(fa.RequiredBy) != 0 {
		t.Fatalf("lock after remove: %+v", kept.Mods)
	}
	if code, _, _ := h.run(t, "build"); code != 0 {
		t.Fatal("lock should not be stale after remove")
	}
}
