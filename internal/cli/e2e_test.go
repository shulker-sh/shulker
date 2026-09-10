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
	return makeJarWith(t, id, filename, env, `"depends":{"fabricloader":">=0.17"}`)
}

func makeJarWith(t *testing.T, id, filename, env, extra string) fakeJar {
	return makeJarVersion(t, id, filename, env, "1.0.0", extra)
}

func makeJarVersion(t *testing.T, id, filename, env, version, extra string) fakeJar {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `{"id":%q,"version":%q,"environment":%q,%s}`, id, version, env, extra)
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
	newer  bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{dir: t.TempDir(), cache: t.TempDir(), jars: map[string]fakeJar{}}
	sodium := makeJar(t, "sodium", "sodium-fabric-0.9.2+mc26.2.jar", "client")
	fabricAPI := makeJar(t, "fabric-api", "fabric-api-0.130.0+26.2.jar", "*")
	h.jars["sodium"], h.jars["fabric-api"] = sodium, fabricAPI
	h.jars["sodium-next"] = makeJarVersion(t, "sodium", "sodium-fabric-0.9.3+mc26.2.jar", "client", "1.1.0", `"depends":{"fabricloader":">=0.17"}`)

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
	mux.HandleFunc("/fabric/versions/loader/26.2/0.17.3/profile/json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": "fabric-loader-0.17.3-26.2", "inheritsFrom": "26.2", "type": "release",
			"mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
			"libraries": []map[string]any{{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.fabricmc.net/"}},
		})
	})
	projects := map[string]map[string]any{
		"sodium":     {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"AANobbMI":   {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"fabric-api": {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
		"P7dR8mSH":   {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
	}
	versionOf := func(id, projectID, number, published string, jar fakeJar, deps []map[string]any) map[string]any {
		return map[string]any{
			"id": id, "project_id": projectID, "version_number": number, "version_type": "release",
			"date_published": published, "game_versions": []string{"26.2"}, "loaders": []string{"fabric"},
			"files":        []map[string]any{{"url": base + "/cdn/" + jar.filename, "filename": jar.filename, "primary": true, "hashes": map[string]string{"sha512": jar.sha512}}},
			"dependencies": deps,
		}
	}
	needsFabricAPI := []map[string]any{{"project_id": "P7dR8mSH", "dependency_type": "required"}}
	versions := func(projectID string) []map[string]any {
		switch projectID {
		case "AANobbMI":
			list := []map[string]any{versionOf("QANobbMI", "AANobbMI", "1.0.0+mc26.2", "2026-09-01T00:00:00Z", h.jars["sodium"], needsFabricAPI)}
			if h.newer {
				list = append(list, versionOf("QANobbM2", "AANobbMI", "1.1.0+mc26.2", "2026-09-05T00:00:00Z", h.jars["sodium-next"], needsFabricAPI))
			}
			return list
		case "P7dR8mSH":
			return []map[string]any{versionOf("Q7dR8mSH", "P7dR8mSH", "1.0.0+mc26.2", "2026-09-01T00:00:00Z", h.jars["fabric-api"], nil)}
		}
		return nil
	}
	mux.HandleFunc("/modrinth/version/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/modrinth/version/")
		for _, projectID := range []string{"AANobbMI", "P7dR8mSH"} {
			for _, v := range versions(projectID) {
				if v["id"] == id {
					writeJSON(w, v)
					return
				}
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/modrinth/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/modrinth/project/")
		if strings.HasSuffix(rest, "/version") {
			if list := versions(strings.TrimSuffix(rest, "/version")); list != nil {
				writeJSON(w, list)
			} else {
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
	added := env.Data.(map[string]any)["added"].([]any)[0].(map[string]any)
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

func TestValidationFailsAndIgnores(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client",
		`"depends":{"fabricloader":">=0.17","fabric-api":">=2.0.0","minecraft":"26.x"},"recommends":{"iris":"*"},"conflicts":{"fabric-api":"1.x"}`)
	h.mustRun(t, "init", "--yes")

	code, stdout, _ := h.run(t, "add", "sodium", "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if code == 0 || env.Error == nil || env.Error.Code != "validation-failed" {
		t.Fatalf("expected validation failure: code=%d env=%+v", code, env)
	}
	if len(env.Error.Candidates) != 1 || !strings.Contains(env.Error.Candidates[0], "sodium 1.0.0 requires fabric-api >=2.0.0, found fabric-api 1.0.0") {
		t.Fatalf("candidates: %v", env.Error.Candidates)
	}
	if !strings.Contains(env.Error.Message, `"declared":">=2.0.0"`) {
		t.Fatalf("message should print the ignore entry: %s", env.Error.Message)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal("lock from init should still exist")
	}
	var l struct {
		Mods map[string]any `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if len(l.Mods) != 0 {
		t.Fatalf("failed add must not write the lock: %v", l.Mods)
	}

	manifestPath := filepath.Join(h.dir, "shulker.json")
	patch := func(ignore string) {
		data, _ := os.ReadFile(manifestPath)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		var entries []any
		_ = json.Unmarshal([]byte(ignore), &entries)
		m["ignore"] = entries
		data, _ = json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patch(`[{"rule":"depends","mod":"sodium","on":"fabric-api","declared":">=1.5.0","note":"old"}]`)
	code, stdout, _ = h.run(t, "add", "sodium", "--json")
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || !strings.Contains(env.Error.Message, "ignore entry 1 is stale") {
		t.Fatalf("stale ignore should re-surface: code=%d %s", code, env.Error.Message)
	}

	patch(`[{"rule":"depends","mod":"sodium","on":"fabric-api","declared":">=2.0.0","note":"works fine in practice"}]`)
	code, stdout, stderr := h.run(t, "add", "sodium", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("matching ignore should pass: %s %s", stdout, stderr)
	}
	data := env.Data.(map[string]any)
	if w := data["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "conflicts with fabric-api 1.x") {
		t.Fatalf("conflict should warn: %v", w)
	}
	if sg := data["suggestions"].([]any); len(sg) != 1 || sg[0] != "sodium recommends iris" {
		t.Fatalf("suggestions: %v", sg)
	}
	stdout = h.mustRun(t, "install")
	if !strings.Contains(stdout, "client: 2 written") {
		t.Fatalf("install: %s", stdout)
	}
}

func TestUpdateOutdatedAndPin(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")

	if out := h.mustRun(t, "outdated"); !strings.Contains(out, "All mods are up to date") {
		t.Fatalf("outdated before a new release: %s", out)
	}
	if out := h.mustRun(t, "update"); !strings.Contains(out, "Already up to date") {
		t.Fatalf("update before a new release: %s", out)
	}

	h.newer = true
	stdout := h.mustRun(t, "outdated", "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	list := env.Data.([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != "sodium" || list[0].(map[string]any)["latest"] != "1.1.0+mc26.2" || list[0].(map[string]any)["pinned"] != false {
		t.Fatalf("outdated: %v", list)
	}

	stdout = h.mustRun(t, "pin", "sodium", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data := env.Data.(map[string]any)
	if data["pin"] != "QANobbMI" || len(data["updated"].([]any)) != 0 {
		t.Fatalf("pin to locked version: %v", data)
	}
	var m struct {
		Mods map[string]struct {
			Pin string `json:"pin"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.json", &m)
	if m.Mods["sodium"].Pin != "QANobbMI" {
		t.Fatalf("manifest pin: %+v", m.Mods)
	}
	if out := h.mustRun(t, "outdated"); !strings.Contains(out, "sodium 1.0.0+mc26.2 -> 1.1.0+mc26.2 (pinned)") {
		t.Fatalf("outdated with pin: %s", out)
	}
	if out := h.mustRun(t, "update"); !strings.Contains(out, "Already up to date") {
		t.Fatalf("update must respect the pin: %s", out)
	}

	stdout = h.mustRun(t, "unpin", "sodium", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data = env.Data.(map[string]any)
	updated := data["updated"].([]any)
	if len(updated) != 1 || updated[0].(map[string]any)["to"] != "1.1.0+mc26.2" || len(data["added"].([]any)) != 0 || len(data["removed"].([]any)) != 0 {
		t.Fatalf("unpin re-resolves: %v", data)
	}
	var l struct {
		Mods map[string]struct {
			Version    string   `json:"version"`
			RequiredBy []string `json:"requiredBy"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if l.Mods["sodium"].Version != "QANobbM2" || len(l.Mods["fabric-api"].RequiredBy) != 1 || l.Mods["fabric-api"].RequiredBy[0] != "sodium" {
		t.Fatalf("lock after unpin: %+v", l.Mods)
	}
	h.readJSON(t, "shulker.json", &m)
	if m.Mods["sodium"].Pin != "" {
		t.Fatalf("manifest still pinned: %+v", m.Mods)
	}

	stdout = h.mustRun(t, "pin", "sodium", "QANobbMI", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if from := env.Data.(map[string]any)["updated"].([]any)[0].(map[string]any)["from"]; from != "1.1.0+mc26.2" {
		t.Fatalf("pin downgrades: %v", env.Data)
	}
	if code, stdout, _ := h.run(t, "pin", "fabric-api", "--json"); code == 0 || !strings.Contains(stdout, `"not-direct"`) {
		t.Fatalf("pinning a dependency: code=%d %s", code, stdout)
	}
	if code, stdout, _ := h.run(t, "pin", "sodium", "Q7dR8mSH", "--json"); code == 0 || !strings.Contains(stdout, `"pin-mismatch"`) {
		t.Fatalf("pinning another project's version: code=%d %s", code, stdout)
	}
	if code, stdout, _ := h.run(t, "update", "fabric-api", "--json"); code == 0 || !strings.Contains(stdout, `"not-direct"`) {
		t.Fatalf("updating a dependency: code=%d %s", code, stdout)
	}
	if code, _, _ := h.run(t, "build"); code != 0 {
		t.Fatal("lock should not be stale after pin")
	}
}

func TestUpdateKeepsPinnedDirectDependency(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium", "fabric-api")
	h.mustRun(t, "pin", "fabric-api")
	h.newer = true

	stdout := h.mustRun(t, "update", "sodium", "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data := env.Data.(map[string]any)
	if updated := data["updated"].([]any); len(updated) != 1 || updated[0].(map[string]any)["id"] != "sodium" {
		t.Fatalf("update sodium: %v", data)
	}
	var l struct {
		Mods map[string]struct {
			Version    string   `json:"version"`
			RequiredBy []string `json:"requiredBy"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if fa := l.Mods["fabric-api"]; fa.Version != "Q7dR8mSH" || len(fa.RequiredBy) != 1 || fa.RequiredBy[0] != "sodium" {
		t.Fatalf("fabric-api after update sodium: %+v", l.Mods)
	}
	if out := h.mustRun(t, "update"); !strings.Contains(out, "Already up to date") {
		t.Fatalf("update all: %s", out)
	}
	h.readJSON(t, "shulker.lock", &l)
	if fa := l.Mods["fabric-api"]; fa.Version != "Q7dR8mSH" || len(fa.RequiredBy) != 1 {
		t.Fatalf("fabric-api after update all: %+v", l.Mods)
	}
}
