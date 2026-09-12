package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/meta"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
	"shulker.sh/shulker/internal/provider/modrinth"
	"shulker.sh/shulker/internal/resolve"
)

type fakeJar struct {
	id, filename string
	data         []byte
	sha512       string
	sha1         string
}

func makeJar(t *testing.T, id, filename string, env string) fakeJar {
	return makeJarWith(t, id, filename, env, `"depends":{"fabricloader":">=0.17"}`)
}

func makeJarWith(t *testing.T, id, filename, env, extra string) fakeJar {
	return makeJarVersion(t, id, filename, env, "1.0.0", extra)
}

func makeJarVersion(t *testing.T, id, filename, env, version, extra string) fakeJar {
	return makeJarFile(t, id, filename, "fabric.mod.json", fmt.Sprintf(`{"id":%q,"version":%q,"environment":%q,%s}`, id, version, env, extra))
}

func makeJarFile(t *testing.T, id, filename, name, content string) fakeJar {
	t.Helper()
	return makeJarFiles(t, id, filename, map[string]string{name: content})
}

func makeJarFiles(t *testing.T, id, filename string, files map[string]string) fakeJar {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, files[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(buf.Bytes())
	sum1 := sha1.Sum(buf.Bytes())
	return fakeJar{id: id, filename: filename, data: buf.Bytes(), sha512: hex.EncodeToString(sum[:]), sha1: hex.EncodeToString(sum1[:])}
}

type harness struct {
	server         *httptest.Server
	jars           map[string]fakeJar
	dir            string
	cache          string
	config         string
	newer          bool
	serverJar      fakeJar
	serverJarHits  int
	quiltLoader    fakeJar
	mixin          fakeJar
	vanilla        fakeJar
	quiltHits      int
	neoInstaller   fakeJar
	neoLibs        map[string]fakeJar
	neoHits        int
	forgeLibs      map[string]fakeJar
	loaders        map[string]fakeLoaderInstall
	forgeInstaller fakeJar
	forgeHits      int
	installs       [][]string
	installErr     error
	stdin          io.Reader
	tty            bool
	runtime        *fakeRuntime
	mojang         map[string]string
	mojangHits     int
	noCurseForge   bool
	cfMods         map[int]*cfMod
	cfHits         int
	ctx            context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{dir: t.TempDir(), cache: t.TempDir(), config: filepath.Join(t.TempDir(), "config.json"), jars: map[string]fakeJar{}, runtime: newFakeRuntime()}
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
	h.vanilla = makeJarFile(t, "minecraft", "server.jar", "version.json", `{"id":"26.2"}`)
	mux.HandleFunc("/piston/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"javaVersion": map[string]any{"component": "java-runtime-epsilon", "majorVersion": 25},
			"downloads":   map[string]any{"server": map[string]any{"url": base + "/piston-data/server.jar", "sha1": h.vanilla.sha1}},
		})
	})
	h.quiltLoader = makeJarFile(t, "quilt_loader", "quilt-loader-0.30.1.jar", "quilt.mod.json",
		`{"schema_version":1,"quilt_loader":{"id":"quilt_loader","version":"0.30.1","provides":[{"id":"fabricloader","version":"0.19.5"}]}}`)
	h.mixin = makeJarFile(t, "mixin", "sponge-mixin-0.17.3.jar", "mixin.txt", "mixin")
	mavenFiles := map[string][]byte{
		"/piston-data/server.jar": h.vanilla.data,
		"/qmaven/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.jar":  h.quiltLoader.data,
		"/fmaven/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17.3.jar": h.mixin.data,
	}
	for path, data := range mavenFiles {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			h.quiltHits++
			w.Write(data)
		})
	}
	mux.HandleFunc("/quilt/versions/loader/26.2/0.30.1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"loader": map[string]any{"maven": "org.quiltmc:quilt-loader:0.30.1", "hashes": map[string]string{"sha512": h.quiltLoader.sha512}}})
	})
	mux.HandleFunc("/quilt/versions/loader/26.2/0.30.1/server/json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": "quilt-loader-0.30.1-26.2", "mainClass": "org.quiltmc.loader.impl.launch.knot.KnotServer",
			"launcherMainClass": "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher",
			"libraries": []map[string]string{
				{"name": "net.fabricmc:sponge-mixin:0.17.3", "url": base + "/fmaven/"},
				{"name": "org.quiltmc:quilt-loader:0.30.1", "url": base + "/qmaven/"},
			},
		})
	})
	mux.HandleFunc("/quilt/versions/loader/26.2/0.30.1/profile/json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": "quilt-loader-0.30.1-26.2", "inheritsFrom": "26.2", "type": "release",
			"mainClass": "org.quiltmc.loader.impl.launch.knot.KnotClient",
			"libraries": []map[string]any{{"name": "org.quiltmc:quilt-loader:0.30.1", "url": base + "/qmaven/"}},
		})
	})
	mux.HandleFunc("/fabric/versions/loader/26.2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"loader": map[string]any{"version": "0.18.0-beta.1", "stable": false}},
			{"loader": map[string]any{"version": "0.17.3", "stable": true}},
			{"loader": map[string]any{"version": "0.17.2", "stable": true}},
		})
	})
	mux.HandleFunc("/quilt/versions/loader/26.2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"loader": map[string]any{"version": "0.20.0-beta.9"}},
			{"loader": map[string]any{"version": "0.30.1"}},
			{"loader": map[string]any{"version": "0.31.0-beta.4"}},
			{"loader": map[string]any{"version": "0.30.0"}},
		})
	})
	mux.HandleFunc("/fabric/versions/loader/26.2/0.17.3/profile/json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": "fabric-loader-0.17.3-26.2", "inheritsFrom": "26.2", "type": "release",
			"mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
			"libraries": []map[string]any{{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.fabricmc.net/"}},
		})
	})
	mux.HandleFunc("/fabric/versions/installer", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"version": "1.2.0-beta.1", "stable": false},
			{"version": "1.1.2", "stable": true},
		})
	})
	mux.HandleFunc("/neoforge/api/maven/versions/releases/net/neoforged/neoforge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"isSnapshot": false, "versions": []string{"26.1.2.40", "26.2.0.56-beta", "26.2.0.87"}})
	})
	mux.HandleFunc("/neoforge/releases/net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-installer.jar", func(w http.ResponseWriter, r *http.Request) {
		h.neoHits++
		w.Write(h.neoInstaller.data)
	})
	mux.HandleFunc("/forge/net/minecraftforge/forge/maven-metadata.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><metadata><versioning><versions>`+
			`<version>26.1-64.0.12</version><version>26.2-65.0.9</version><version>26.2-65.1.3</version>`+
			`<version>26.2-65.1.4-1.26.x</version>`+
			`</versions></versioning></metadata>`)
	})
	mux.HandleFunc("/forge/net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-installer.jar", func(w http.ResponseWriter, r *http.Request) {
		h.forgeHits++
		w.Write(h.forgeInstaller.data)
	})
	h.neoLibs = map[string]fakeJar{
		"net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar": makeJarFile(t, "neoforge", "neoforge-26.2.0.87-universal.jar", "META-INF/neoforge.mods.toml", "[[mods]]\nmodId=\"neoforge\"\n"),
		"org/ow2/asm/asm/9.10.1/asm-9.10.1.jar":                             makeJarFile(t, "asm", "asm-9.10.1.jar", "asm.txt", "asm"),
	}
	h.forgeLibs = map[string]fakeJar{
		"net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar": makeJarFile(t, "forge", "forge-26.2-65.1.3-universal.jar", "META-INF/mods.toml", "[[mods]]\nmodId=\"forge\"\n"),
	}
	mux.HandleFunc("/neomaven/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/neomaven/")
		jar, ok := h.neoLibs[path]
		if !ok {
			if jar, ok = h.forgeLibs[path]; ok {
				h.forgeHits++
			}
		} else {
			h.neoHits++
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(jar.data)
	})
	h.serverJar = makeJar(t, "fabric-server-launch", "fabric-server-launch.jar", "server")
	mux.HandleFunc("/fabric/versions/loader/26.2/0.17.3/1.1.2/server/jar", func(w http.ResponseWriter, r *http.Request) {
		h.serverJarHits++
		w.Write(h.serverJar.data)
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
	mux.HandleFunc("/modrinth/version_file/", func(w http.ResponseWriter, r *http.Request) {
		sha1 := strings.TrimPrefix(r.URL.Path, "/modrinth/version_file/")
		for _, projectID := range []string{"AANobbMI", "P7dR8mSH"} {
			for _, v := range versions(projectID) {
				for _, jar := range h.jars {
					if jar.sha1 == sha1 && v["files"].([]map[string]any)[0]["filename"] == jar.filename {
						writeJSON(w, v)
						return
					}
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
	h.mojang = map[string]string{}
	mux.HandleFunc("/mojang/profiles/minecraft", func(w http.ResponseWriter, r *http.Request) {
		h.mojangHits++
		var names []string
		_ = json.NewDecoder(r.Body).Decode(&names)
		profiles := []map[string]string{}
		for _, n := range names {
			for name, id := range h.mojang {
				if strings.EqualFold(name, n) {
					profiles = append(profiles, map[string]string{"id": strings.ReplaceAll(id, "-", ""), "name": name})
				}
			}
		}
		writeJSON(w, profiles)
	})
	mux.HandleFunc("/session/session/minecraft/profile/", func(w http.ResponseWriter, r *http.Request) {
		h.mojangHits++
		want := strings.TrimPrefix(r.URL.Path, "/session/session/minecraft/profile/")
		for name, id := range h.mojang {
			if strings.ReplaceAll(id, "-", "") == want {
				writeJSON(w, map[string]string{"id": want, "name": name})
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h.runtime.register(mux, func() string { return base })
	h.registerCurseForge(t, mux, func() string { return base })
	h.server = httptest.NewServer(mux)
	base = h.server.URL
	library := func(name, path string) map[string]any {
		jar, ok := h.neoLibs[path]
		if !ok {
			jar = h.forgeLibs[path]
		}
		return map[string]any{"name": name, "downloads": map[string]any{"artifact": map[string]any{"path": path, "url": base + "/neomaven/" + path, "sha1": jar.sha1}}}
	}
	profile, _ := json.Marshal(map[string]any{"minecraft": "26.2", "libraries": []any{
		library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
		map[string]any{"name": "net.neoforged:bundled:1.0", "downloads": map[string]any{"artifact": map[string]any{"path": "net/neoforged/bundled/1.0/bundled-1.0.jar", "url": ""}}},
	}})
	version, _ := json.Marshal(map[string]any{"libraries": []any{
		library("net.neoforged:neoforge:26.2.0.87:universal", "net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar"),
		library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
	}})
	h.neoInstaller = makeJarFiles(t, "neoforge-installer", "neoforge-26.2.0.87-installer.jar", map[string]string{"install_profile.json": string(profile), "version.json": string(version)})
	forgeProfile, _ := json.Marshal(map[string]any{"minecraft": "26.2", "libraries": []any{
		library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
	}})
	forgeVersion, _ := json.Marshal(map[string]any{"libraries": []any{
		library("net.minecraftforge:forge:26.2-65.1.3:universal", "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar"),
	}})
	h.forgeInstaller = makeJarFiles(t, "forge-installer", "forge-26.2-65.1.3-installer.jar", map[string]string{"install_profile.json": string(forgeProfile), "version.json": string(forgeVersion)})
	// The vanilla jar name comes off the real table, so the fake can't disagree with the code
	// about which name a loader's installer looks for.
	vanillaLib := func(name string) string {
		l, _ := loader.Lookup(name)
		jar := "server-26.2"
		if l.MinecraftJarClassifier != "" {
			jar += "-" + l.MinecraftJarClassifier
		}
		return "net/minecraft/server/26.2/" + jar + ".jar"
	}
	h.loaders = map[string]fakeLoaderInstall{
		"neoforge": {
			installer: h.neoInstaller, version: "26.2.0.87", versionID: "neoforge-26.2.0.87", profileKey: "NeoForge",
			libs: append(slices.Sorted(maps.Keys(h.neoLibs)), vanillaLib("neoforge")),
		},
		"forge": {
			installer: h.forgeInstaller, version: "65.1.3", versionID: "26.2-forge-65.1.3", profileKey: "forge",
			libs: []string{"org/ow2/asm/asm/9.10.1/asm-9.10.1.jar", "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar", vanillaLib("forge")},
		},
	}
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
	a.configPath = h.config
	a.stdin = h.stdin
	a.tty = func() bool { return h.tty }
	a.installer = h.fakeInstaller
	f := fetch.New("test")
	piston := meta.NewPiston(f)
	piston.ManifestURL = h.server.URL + "/piston/manifest.json"
	fabric := meta.NewFabric(f)
	fabric.BaseURL = h.server.URL + "/fabric"
	quilt := meta.NewQuilt(f)
	quilt.BaseURL = h.server.URL + "/quilt"
	quilt.MavenURL = h.server.URL + "/qmaven"
	neoforge := meta.NewNeoForge(f)
	neoforge.BaseURL = h.server.URL + "/neoforge"
	forge := meta.NewForge(f)
	forge.BaseURL = h.server.URL + "/forge"
	mr := modrinth.New(f)
	mr.BaseURL = h.server.URL + "/modrinth"
	runtimes := meta.NewRuntimes(f)
	runtimes.IndexURL = h.server.URL + "/jrt/all.json"
	players := player.New(f)
	players.APIURL = h.server.URL + "/mojang"
	players.SessionURL = h.server.URL + "/session"
	providers := map[string]provider.Provider{"modrinth": mr}
	if !h.noCurseForge {
		cf := curseforge.New(f, curseForgeTestKey)
		cf.BaseURL = h.server.URL + "/curseforge"
		providers["curseforge"] = cf
	}
	c := &cache.Cache{Dir: h.cache}
	a.d = &deps{
		fetch:     f,
		cache:     c,
		providers: providers,
		meta:      &resolve.Meta{Piston: piston, Fabric: fabric, Quilt: quilt, NeoForge: neoforge, Forge: forge, Cache: c},
		runtimes:  runtimes,
		players:   players,
	}
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	code := a.run(ctx, args)
	return code, stdout.String(), stderr.String()
}

func (h *harness) mustRunStderr(t *testing.T, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr := h.run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout, stderr
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
	added := env.Data.(map[string]any)["added"].([]any)
	dep, direct := added[0].(map[string]any), added[1].(map[string]any)
	if len(added) != 2 || direct["id"] != "sodium" || direct["side"] != "client" || len(direct["requiredBy"].([]any)) != 0 || dep["id"] != "fabric-api" || dep["requiredBy"].([]any)[0] != "sodium" {
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
	if !strings.Contains(stdout, "Fetched 0 file(s).") || !strings.Contains(stdout, "client: 4 written") {
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
	if state.Target != "client" || len(state.Files) != 4 {
		t.Fatalf("state: %+v", state)
	}

	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "0 written, 4 unchanged") {
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
	if env.Error == nil || env.Error.Code != "build-conflict" || len(env.Error.Items) != 1 {
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
	if !strings.Contains(stdout, "0 written, 4 unchanged, 0 kept, 0 removed") {
		t.Fatalf("removed override should hand options.txt back to client.options: %s", stdout)
	}
	if data, _ := os.ReadFile(options); string(data) != "renderDistance:16\nlang:en_us\njoinedFirstServer:true\nonboardAccessibility:false\nskipMultiplayerWarning:true\ntutorialStep:none\n" {
		t.Fatalf("options.txt after override removal: %q", data)
	}

	extra := filepath.Join(h.dir, "overrides", "config", "extra.txt")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 removed") {
		t.Fatalf("removed source should delete build file: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "config", "extra.txt")); !os.IsNotExist(err) {
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

func TestStaleLockWarnsAndBuilds(t *testing.T) {
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
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "shulker.lock is out of date") {
		t.Fatalf("code=%d env=%+v", code, env)
	}
	code, stdout, _ = h.run(t, "export", "mrpack", "--version", "1.0.0", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || !env.LockStale || env.Error.Code != "lock-stale" {
		t.Fatalf("export must refuse a stale lock: code=%d env=%+v", code, env)
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "shulker.lock is out of date") {
		t.Fatalf("install warns about the stale lock in the envelope: %+v", env)
	}
}

func TestRemovePrunesOrphans(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "remove", "fabric-api", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "not-direct" || env.Error.Items[0] != "sodium" {
		t.Fatalf("removing a dependency: code=%d env=%+v", code, env)
	}
	if code, stdout, _ = h.run(t, "remove", "nope", "--json"); code == 0 || !strings.Contains(stdout, `"mod-not-found"`) {
		t.Fatalf("removing an unknown mod: code=%d %s", code, stdout)
	}

	stdout = h.mustRun(t, "remove", "sodium", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	data := env.Data.(map[string]any)
	removed := data["removed"].([]any)
	if !env.OK || env.LockStale || len(removed) != 2 || removed[0].(map[string]any)["id"] != "fabric-api" || removed[0].(map[string]any)["requiredBy"].([]any)[0] != "sodium" || removed[1].(map[string]any)["id"] != "sodium" {
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
	if strings.Contains(stdout, "fabric-api") {
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

func TestRemoveValidatesBeforeSaving(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium", "fabric-api")
	lockBefore, _ := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	manifestBefore, _ := os.ReadFile(filepath.Join(h.dir, "shulker.json"))

	code, stdout, _ := h.run(t, "remove", "fabric-api", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "validation-failed" {
		t.Fatalf("removing a jar dependency: code=%d %s", code, stdout)
	}
	lockAfter, _ := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))
	manifestAfter, _ := os.ReadFile(filepath.Join(h.dir, "shulker.json"))
	if !bytes.Equal(lockBefore, lockAfter) || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("a failed remove must not write the manifest or lock")
	}
}

func TestInterruptedCommandFails(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	lockBefore, _ := os.ReadFile(filepath.Join(h.dir, "shulker.lock"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.ctx = ctx
	code, stdout, _ := h.run(t, "add", "sodium", "--json")
	if e := failureCode(t, stdout); code != out.ExitInterrupted || e.Code != "interrupted" {
		t.Fatalf("add after an interrupt: code=%d %s", code, stdout)
	}
	if lockAfter, _ := os.ReadFile(filepath.Join(h.dir, "shulker.lock")); !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("an interrupted add must not write the lock")
	}
}

func TestInitChecksTheLoader(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "init", "--yes", "--loader", "rift", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || len(e.Candidates) != 4 {
		t.Fatalf("unknown loader: code=%d %s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "shulker.json")); err == nil {
		t.Fatal("a failed init must not write shulker.json")
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
	if len(env.Error.Items) != 1 || !strings.Contains(env.Error.Items[0], "sodium 1.0.0 requires fabric-api >=2.0.0, found fabric-api 1.0.0") {
		t.Fatalf("candidates: %v", env.Error.Items)
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
	if w := env.Warnings; len(w) != 1 || !strings.Contains(w[0], "conflicts with fabric-api 1.x") {
		t.Fatalf("conflict should warn: %v", w)
	}
	if sg := data["suggestions"].([]any); len(sg) != 1 || sg[0] != "sodium recommends iris" {
		t.Fatalf("suggestions: %v", sg)
	}
	stdout = h.mustRun(t, "install")
	if !strings.Contains(stdout, "client: 4 written") {
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

func TestDiffAndPull(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")
	overrides := filepath.Join(h.dir, "overrides", "config")
	if err := os.MkdirAll(overrides, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"plain.txt": "a=1\n", "tpl.txt.tmpl": "v=1\n"} {
		if err := os.WriteFile(filepath.Join(overrides, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.mustRun(t, "install")
	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "client: no changes in the build directory") {
		t.Fatalf("clean diff: %s", stdout)
	}

	buildDir := filepath.Join(h.dir, "build", "client")
	write := func(dir, rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(buildDir, "config/plain.txt", "a=2\n")
	write(buildDir, "config/tpl.txt", "v=2\n")
	write(buildDir, "config/new.txt", "hand\n")
	write(h.dir, "overrides/config/new.txt", "source\n")
	options, _ := os.ReadFile(filepath.Join(buildDir, "options.txt"))
	write(buildDir, "options.txt", strings.Replace(string(options), "joinedFirstServer:true", "joinedFirstServer:false", 1)+"foo:bar\n")

	stdout := h.mustRun(t, "diff", "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	files := env.Data.([]any)[0].(map[string]any)["files"].([]any)
	states := map[string]string{}
	diffs := map[string]string{}
	for _, f := range files {
		m := f.(map[string]any)
		states[m["path"].(string)] = m["state"].(string)
		diffs[m["path"].(string)] = m["diff"].(string)
	}
	want := map[string]string{"config/new.txt": "untracked", "config/plain.txt": "kept", "config/tpl.txt": "kept", "options.txt": "kept"}
	if len(states) != len(want) {
		t.Fatalf("diff states: %v", states)
	}
	for p, s := range want {
		if states[p] != s {
			t.Fatalf("diff state of %s: %q want %q (%v)", p, states[p], s, states)
		}
	}
	if d := diffs["options.txt"]; !strings.Contains(d, "-joinedFirstServer:false\n+joinedFirstServer:true\n") || strings.Contains(d, "foo") {
		t.Fatalf("options diff should cover managed keys only: %s", d)
	}
	if d := diffs["config/plain.txt"]; !strings.Contains(d, "@@ -1 +1 @@\n-a=2\n+a=1\n") {
		t.Fatalf("plain diff: %s", d)
	}

	stdout = h.mustRun(t, "pull")
	for _, line := range []string{
		"client: 1 file(s) pulled, 1 key(s) written to shulker.json, 2 skipped",
		"pulled config/plain.txt -> overrides/config/plain.txt",
		"set options.txt joinedFirstServer=false",
		"skipped config/new.txt (not written by shulker; name it to adopt it)",
		"skipped config/tpl.txt (rendered from template overrides/config/tpl.txt.tmpl)",
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("pull output missing %q:\n%s", line, stdout)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(overrides, "plain.txt")); string(data) != "a=2\n" {
		t.Fatalf("pulled override: %q", data)
	}
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	if v := m["client"].(map[string]any)["options"].(map[string]any)["joinedFirstServer"]; v != false {
		t.Fatalf("manifest option should be the bool false: %#v", v)
	}

	if code, _, stderr := h.run(t, "pull", "config/missing.txt"); code == 0 || !strings.Contains(stderr, "is not changed in the build directory") {
		t.Fatalf("pull of an unchanged path should fail: %d %s", code, stderr)
	}
	stdout = h.mustRun(t, "pull", "config/new.txt")
	if !strings.Contains(stdout, "pulled config/new.txt -> overrides/config/new.txt") {
		t.Fatalf("named untracked file should be adopted: %s", stdout)
	}
	if data, _ := os.ReadFile(filepath.Join(overrides, "new.txt")); string(data) != "hand\n" {
		t.Fatalf("adopted override: %q", data)
	}

	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 written, 5 unchanged, 1 kept, 0 removed") || !strings.Contains(stdout, "kept config/tpl.txt") {
		t.Fatalf("build after pull: %s", stdout)
	}
	if data, _ := os.ReadFile(filepath.Join(buildDir, "options.txt")); !strings.Contains(string(data), "joinedFirstServer:false\n") || !strings.Contains(string(data), "foo:bar\n") {
		t.Fatalf("options after pull: %q", data)
	}

	write(h.dir, "overrides/config/plain.txt", "a=3\n")
	write(buildDir, "config/plain.txt", "a=4\n")
	stdout = h.mustRun(t, "diff")
	if !strings.Contains(stdout, "conflict config/plain.txt\n") || !strings.Contains(stdout, "-a=4\n+a=3\n") {
		t.Fatalf("conflict diff: %s", stdout)
	}
	h.mustRun(t, "pull", "config/plain.txt")
	if data, _ := os.ReadFile(filepath.Join(overrides, "plain.txt")); string(data) != "a=4\n" {
		t.Fatalf("pulled conflict should take the build file: %q", data)
	}
	if stdout = h.mustRun(t, "build"); !strings.Contains(stdout, "0 written, 6 unchanged, 1 kept") {
		t.Fatalf("build after conflict pull: %s", stdout)
	}
}

// fakeLoaderInstall is what the harness pretends each loader's installer ships and generates.
type fakeLoaderInstall struct {
	installer  fakeJar
	version    string
	versionID  string
	profileKey string
	libs       []string
}

func installerLoader(flag string) (loader.Loader, bool) {
	for _, l := range loader.All {
		if flag == l.InstallServerFlag || flag == l.InstallClientFlag {
			return l, true
		}
	}
	return loader.Loader{}, false
}

// fakeInstaller stands in for NeoForge's and Forge's installers: on a server it checks that the
// build placed what the installer would download offline, then writes what its processors
// generate; on a client it installs into the launcher.
func (h *harness) fakeInstaller(_ context.Context, java, jar string, args []string) error {
	h.installs = append(h.installs, args)
	if h.installErr != nil {
		return h.installErr
	}
	if _, err := os.Stat(java); err != nil {
		return err
	}
	l, ok := installerLoader(args[0])
	if !ok {
		return fmt.Errorf("installer args %v", args)
	}
	fake := h.loaders[l.Name]
	if got, err := os.ReadFile(jar); err != nil || string(got) != string(fake.installer.data) {
		return fmt.Errorf("installer jar %s is not the locked %s one (%v)", jar, l.Name, err)
	}
	if args[0] == l.InstallClientFlag {
		return h.fakeClientInstall(args, fake)
	}
	if len(args) != 3 || args[2] != "--offline" {
		return fmt.Errorf("installer args %v", args)
	}
	dir := args[1]
	for _, rel := range fake.libs {
		if _, err := os.Stat(filepath.Join(dir, "libraries", filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("offline install without a download: %w", err)
		}
	}
	argsDir := "libraries/" + l.MavenPath + "/" + l.ArtifactVersion("26.2", fake.version)
	files := map[string]string{
		argsDir + "/unix_args.txt":      "-DlibraryDirectory=libraries\n",
		argsDir + "/win_args.txt":       "-DlibraryDirectory=libraries\n",
		argsDir + "/server-patched.jar": "patched",
		"run.sh":                        "java @user_jvm_args.txt @" + argsDir + "/unix_args.txt \"$@\"\n",
		"user_jvm_args.txt":             "# JVM arguments\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// fakeClientInstall stands in for the installer's client install: it writes the version it
// installed into the launcher and, like the real one, leaves a launcher profile of its own behind.
func (h *harness) fakeClientInstall(args []string, fake fakeLoaderInstall) error {
	if len(args) != 2 {
		return fmt.Errorf("installer args %v", args)
	}
	dir, id := args[1], fake.versionID
	if err := os.MkdirAll(filepath.Join(dir, "versions", id), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "versions", id, id+".json"), []byte(`{"id":"`+id+`"}`), 0o644); err != nil {
		return err
	}
	path := filepath.Join(dir, "launcher_profiles.json")
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	profiles := map[string]json.RawMessage{}
	if raw, ok := top["profiles"]; ok {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return err
		}
	}
	profiles[fake.profileKey] = json.RawMessage(`{"name":"` + fake.profileKey + `","type":"custom","lastVersionId":"` + id + `"}`)
	if top["profiles"], err = json.Marshal(profiles); err != nil {
		return err
	}
	written, err := json.Marshal(top)
	if err != nil {
		return err
	}
	return os.WriteFile(path, written, 0o644)
}
