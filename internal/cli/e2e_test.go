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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fetch/fetchtest"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/curseforge"
	"shulker.sh/shulker/internal/provider/modrinth"
	"shulker.sh/shulker/internal/selfupdate"
	"shulker.sh/shulker/schema"
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
	server     *httptest.Server
	cdnDown    map[string]bool
	cdnCut     map[string]bool
	cdnDrop    map[string]bool
	jars       map[string]fakeJar
	dir        string
	cache      string
	config     string
	home       string
	newer      bool
	sodiumBeta bool
	// now is the time release ages are measured to; zero is the clock.
	now            time.Time
	newerAPI       bool
	serverJar      fakeJar
	quiltLoader    fakeJar
	quiltLaunch    fakeJar
	mixin          fakeJar
	vanilla        fakeJar
	clientJar      fakeJar
	build          *selfupdate.Build
	brigadier      fakeJar
	fabricLoader   fakeJar
	assetIndex     []byte
	assets         map[string]string
	noQuickPlay    bool
	noRanges       bool
	hitsMu         sync.Mutex
	storeHits      int
	cdnHits        int
	neoInstaller   fakeJar
	neoLibs        map[string]fakeJar
	forgeLibs      map[string]fakeJar
	loaders        map[string]fakeLoaderInstall
	forgeInstaller fakeJar
	installs       [][]string
	installErr     error
	stdin          io.Reader
	tty            bool
	runtime        *fakeRuntime
	mojang         map[string]string
	mojangHits     int
	noCurseForge   bool
	cfSearchFails  bool
	cfMods         map[int]*cfMod
	cfHits         int
	// modrinthBatches counts version_files and projects requests.
	modrinthBatches int
	// modrinthPacks are the modpack projects the Modrinth fake knows, by project id.
	modrinthPacks map[string]*modrinthPack
	// modrinthNoSha1 has the Modrinth fake publish only a file's sha512.
	modrinthNoSha1 bool
	ctx            context.Context
	msa            *fakeMSA
	// exe stands in for the running binary, for the commands that move or remove it.
	exe string
	// watching counts the watchers a launch left running, so a test's directories outlive the runs
	// they are still recording.
	watching sync.WaitGroup
	// grace is the watcher's early-exit wait, 0 so a launch returns at once unless a test asks.
	grace time.Duration
	// installerFetchedClient says a client install found no vanilla jar and downloaded one itself.
	installerFetchedClient bool
}

// watch stands in for the watcher process, because a test binary re-execed is a test binary and not
// shulker. The run is watched here instead, on a goroutine that outlives the command that started
// it exactly as the watcher outlives it.
func (h *harness) watch(req game.Launch) (int, error) {
	started := make(chan watchReply, 1)
	h.watching.Add(1)
	go func() {
		defer h.watching.Done()
		h.newApp(io.Discard, io.Discard).watchRun(req, nil, h.grace, func(r watchReply) { started <- r })
	}()
	r := <-started
	if r.Error != "" {
		return 0, notStarted(r.Error)
	}
	if r.Exited {
		return 0, exitedAtStart(r, req.Log)
	}
	return r.PID, nil
}

// hit counts one request to a fake. Downloads run several at a time, so every counter a handler
// bumps goes through here.
func (h *harness) hit(counter *int) {
	h.hitsMu.Lock()
	defer h.hitsMu.Unlock()
	*counter++
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{dir: t.TempDir(), cache: t.TempDir(), config: filepath.Join(t.TempDir(), "config.json"), home: t.TempDir(), jars: map[string]fakeJar{}, runtime: newFakeRuntime()}
	// A watcher writes its record after the command that spawned it has returned, so the test waits
	// for it before its directories go.
	t.Cleanup(h.watching.Wait)
	sodium := makeJar(t, "sodium", "sodium-fabric-0.9.2+mc26.2.jar", "client")
	fabricAPI := makeJar(t, "fabric-api", "fabric-api-0.130.0+26.2.jar", "*")
	h.jars["sodium"], h.jars["fabric-api"] = sodium, fabricAPI
	h.jars["sodium-next"] = makeJarVersion(t, "sodium", "sodium-fabric-0.9.3+mc26.2.jar", "client", "1.1.0", `"depends":{"fabricloader":">=0.17"}`)
	h.jars["fabric-api-next"] = makeJarVersion(t, "fabric-api", "fabric-api-0.140.0+26.2.jar", "*", "2.0.0", `"depends":{"fabricloader":">=0.17"}`)
	h.jars["fresh-animations"] = makeJarFile(t, "fresh-animations", "FreshAnimations_v1.9.4.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"fresh"}}`)
	h.jars["complementary"] = makeJarFile(t, "complementary", "ComplementaryReimagined_r5.5.1.zip", "shaders/gbuffers_basic.vsh", "// shader")
	h.jars["irisshaders"] = makeJar(t, "iris", "iris-fabric-1.11.3+mc26.2.jar", "client")
	h.jars["autoslabs"] = makeJarFiles(t, "autoslabs", "AutoslabsCompat.zip", map[string]string{"pack.mcmeta": `{"pack":{"pack_format":15,"description":"autoslabs"}}`, "data/autoslabs/tags/block/slabs.json": "{}", "assets/autoslabs/lang/en_us.json": "{}"})
	h.jars["terralith"] = makeJarFiles(t, "terralith", "Terralith_26.2_v2.6.4.zip", map[string]string{"pack.mcmeta": `{"pack":{"pack_format":107,"description":"terralith"}}`, "data/terralith/worldgen/biome/moonlight_grove.json": "{}"})

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
	h.vanilla = makeJarFile(t, "minecraft", "server.jar", "version.json", `{"id":"26.2","world_version":4903}`)
	h.clientJar = makeJarFile(t, "minecraft", "client.jar", "version.json", `{"id":"26.2"}`)
	h.brigadier = makeJarFile(t, "brigadier", "brigadier-1.3.10.jar", "brigadier.txt", "brigadier")
	h.fabricLoader = makeJarFile(t, "fabric_loader", "fabric-loader-0.17.3.jar", "fabric.mod.json", `{"id":"fabricloader"}`)
	h.assets = map[string]string{"icons/icon_16x16.png": "icon bytes", "minecraft/lang/en_us.json": "{}"}
	objects := map[string]any{}
	for name, body := range h.assets {
		objects[name] = map[string]any{"hash": sha1Hex([]byte(body)), "size": len(body)}
	}
	h.assetIndex, _ = json.Marshal(map[string]any{"objects": objects})
	mux.HandleFunc("/piston/", func(w http.ResponseWriter, r *http.Request) {
		gameArgs := []any{"--username", "${auth_player_name}", "--accessToken", "${auth_access_token}"}
		if !h.noQuickPlay {
			gameArgs = append(gameArgs,
				map[string]any{"rules": []any{map[string]any{"action": "allow", "features": map[string]bool{"is_quick_play_singleplayer": true}}}, "value": []string{"--quickPlaySingleplayer", "${quickPlaySingleplayer}"}},
				map[string]any{"rules": []any{map[string]any{"action": "allow", "features": map[string]bool{"is_quick_play_multiplayer": true}}}, "value": []string{"--quickPlayMultiplayer", "${quickPlayMultiplayer}"}},
			)
		}
		writeJSON(w, map[string]any{
			"id":          strings.TrimSuffix(filepath.Base(r.URL.Path), ".json"),
			"type":        "release",
			"mainClass":   "net.minecraft.client.main.Main",
			"libraries":   []map[string]any{{"name": "com.mojang:brigadier:1.3.10", "downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar", "url": base + "/mojang-libs/brigadier-1.3.10.jar", "sha1": h.brigadier.sha1, "size": len(h.brigadier.data)}}}},
			"javaVersion": map[string]any{"component": "java-runtime-epsilon", "majorVersion": 25},
			"assetIndex":  map[string]any{"id": "26", "url": base + "/piston/assets/26.json", "sha1": sha1Hex(h.assetIndex), "size": len(h.assetIndex), "totalSize": 21},
			"arguments":   map[string]any{"game": gameArgs, "jvm": []string{"-Djava.library.path=${natives_directory}", "-cp", "${classpath}"}},
			"downloads": map[string]any{
				"server": map[string]any{"url": base + "/piston-data/server.jar", "sha1": h.vanilla.sha1},
				"client": map[string]any{"url": base + "/piston-data/client.jar", "sha1": h.clientJar.sha1, "size": len(h.clientJar.data)},
			},
		})
	})
	mux.HandleFunc("/piston/assets/26.json", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.storeHits)
		w.Write(h.assetIndex)
	})
	mux.HandleFunc("/piston-data/client.jar", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.storeHits)
		w.Write(h.clientJar.data)
	})
	mux.HandleFunc("/mojang-libs/brigadier-1.3.10.jar", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.storeHits)
		w.Write(h.brigadier.data)
	})
	mux.HandleFunc("/resources/", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.storeHits)
		for _, body := range h.assets {
			if strings.HasSuffix(r.URL.Path, sha1Hex([]byte(body))) {
				io.WriteString(w, body)
				return
			}
		}
		http.NotFound(w, r)
	})
	h.quiltLoader = makeJarFile(t, "quilt_loader", "quilt-loader-0.30.1.jar", "quilt.mod.json",
		`{"schema_version":1,"quilt_loader":{"id":"quilt_loader","version":"0.30.1","provides":[{"id":"fabricloader","version":"0.19.5"}]}}`)
	h.mixin = makeJarFile(t, "mixin", "sponge-mixin-0.17.3.jar", "mixin.txt", "mixin")
	h.quiltLaunch = makeJarFiles(t, "quilt-server-launch", "quilt-server-launch.jar", map[string]string{
		"META-INF/MANIFEST.MF":           "Manifest-Version: 1.0\r\nMain-Class: org.quiltmc.loader.impl.launch.server.QuiltServerLauncher\r\nClass-Path: libraries/net/fabricmc/sponge-mixin/0.17.3/sponge-mixin-0.17\r\n .3.jar libraries/org/quiltmc/quilt-loader/0.30.1/quilt-loader-0.30.1.ja\r\n r\r\n\r\n",
		"quilt-server-launch.properties": "launch.mainClass=org.quiltmc.loader.impl.launch.knot.KnotServer\n",
	})
	mux.HandleFunc("/piston-data/server.jar", func(w http.ResponseWriter, r *http.Request) {
		if !h.noRanges && (r.Method == http.MethodHead || r.Header.Get("Range") != "") {
			http.ServeContent(w, r, "server.jar", time.Time{}, bytes.NewReader(h.vanilla.data))
			return
		}
		w.Write(h.vanilla.data)
	})
	h.neoLibs = map[string]fakeJar{
		"net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar": makeJarFile(t, "neoforge", "neoforge-26.2.0.87-universal.jar", "META-INF/neoforge.mods.toml", "[[mods]]\nmodId=\"neoforge\"\n"),
		"org/ow2/asm/asm/9.10.1/asm-9.10.1.jar":                             makeJarFile(t, "asm", "asm-9.10.1.jar", "asm.txt", "asm"),
	}
	h.forgeLibs = map[string]fakeJar{
		"net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar": makeJarFile(t, "forge", "forge-26.2-65.1.3-universal.jar", "META-INF/mods.toml", "[[mods]]\nmodId=\"forge\"\n"),
	}
	h.serverJar = makeJar(t, "fabric-server-launch", "fabric-server-launch.jar", "server")
	projects := map[string]map[string]any{
		"sodium":                   {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"AANobbMI":                 {"id": "AANobbMI", "slug": "sodium", "title": "Sodium", "client_side": "required", "server_side": "unsupported"},
		"fabric-api":               {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
		"P7dR8mSH":                 {"id": "P7dR8mSH", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
		"fresh-animations":         {"id": "50dA9Sha", "slug": "fresh-animations", "title": "Fresh Animations", "client_side": "required", "server_side": "unsupported", "project_type": "resourcepack"},
		"50dA9Sha":                 {"id": "50dA9Sha", "slug": "fresh-animations", "title": "Fresh Animations", "client_side": "required", "server_side": "unsupported", "project_type": "resourcepack"},
		"complementary-reimagined": {"id": "HVnmMxH1", "slug": "complementary-reimagined", "title": "Complementary Reimagined", "client_side": "required", "server_side": "unsupported", "project_type": "shader"},
		"HVnmMxH1":                 {"id": "HVnmMxH1", "slug": "complementary-reimagined", "title": "Complementary Reimagined", "client_side": "required", "server_side": "unsupported", "project_type": "shader"},
		"irisshaders":              {"id": "YL57xq9U", "slug": "irisshaders", "title": "Iris Shaders", "client_side": "required", "server_side": "unsupported"},
		"YL57xq9U":                 {"id": "YL57xq9U", "slug": "irisshaders", "title": "Iris Shaders", "client_side": "required", "server_side": "unsupported"},
		"terralith":                {"id": "8oi3bsk5", "slug": "terralith", "title": "Terralith", "client_side": "optional", "server_side": "required", "project_type": "mod", "loaders": []string{"datapack"}},
		"8oi3bsk5":                 {"id": "8oi3bsk5", "slug": "terralith", "title": "Terralith", "client_side": "optional", "server_side": "required", "project_type": "mod", "loaders": []string{"datapack"}},
		// Illustrative ids: a datapack whose zip also carries assets/, as BMC2's AutoslabsCompat does.
		"autoslabs-compat-(bmc)": {"id": "AutoSlb1", "slug": "autoslabs-compat-(bmc)", "title": "Autoslabs Compat", "client_side": "optional", "server_side": "required", "project_type": "mod", "loaders": []string{"datapack"}},
		"AutoSlb1":               {"id": "AutoSlb1", "slug": "autoslabs-compat-(bmc)", "title": "Autoslabs Compat", "client_side": "optional", "server_side": "required", "project_type": "mod", "loaders": []string{"datapack"}},
	}
	versionTagged := func(id, projectID, number, published string, jar fakeJar, deps []map[string]any, loaders []string) map[string]any {
		hashes := map[string]string{"sha512": jar.sha512, "sha1": jar.sha1}
		if h.modrinthNoSha1 {
			delete(hashes, "sha1")
		}
		return map[string]any{
			"id": id, "project_id": projectID, "version_number": number, "version_type": "release",
			"date_published": published, "game_versions": []string{"26.2"}, "loaders": loaders,
			"files":        []map[string]any{{"url": "https://cdn.modrinth.com/cdn/" + jar.filename, "filename": jar.filename, "primary": true, "hashes": hashes, "size": len(jar.data)}},
			"dependencies": deps,
		}
	}
	versionOf := func(id, projectID, number, published string, jar fakeJar, deps []map[string]any) map[string]any {
		return versionTagged(id, projectID, number, published, jar, deps, []string{"fabric"})
	}
	needsFabricAPI := []map[string]any{{"project_id": "P7dR8mSH", "dependency_type": "required"}}
	versions := func(projectID string) []map[string]any {
		switch projectID {
		case "AANobbMI":
			list := []map[string]any{versionOf("QANobbMI", "AANobbMI", "1.0.0+mc26.2", "2026-09-01T00:00:00Z", h.jars["sodium"], needsFabricAPI)}
			if h.sodiumBeta {
				list[0]["version_type"] = "beta"
			}
			if h.newer {
				list = append(list, versionOf("QANobbM2", "AANobbMI", "1.1.0+mc26.2", "2026-09-05T00:00:00Z", h.jars["sodium-next"], needsFabricAPI))
			}
			return list
		case "P7dR8mSH":
			list := []map[string]any{versionOf("Q7dR8mSH", "P7dR8mSH", "1.0.0+mc26.2", "2026-09-01T00:00:00Z", h.jars["fabric-api"], nil)}
			if h.newerAPI {
				list = append(list, versionOf("Q7dR8mS2", "P7dR8mSH", "2.0.0+mc26.2", "2026-09-05T00:00:00Z", h.jars["fabric-api-next"], nil))
			}
			return list
		case "50dA9Sha":
			return []map[string]any{versionTagged("Vb7Kq2Xn", "50dA9Sha", "1.9.4", "2026-09-01T00:00:00Z", h.jars["fresh-animations"], nil, []string{"minecraft"})}
		case "HVnmMxH1":
			return []map[string]any{versionTagged("pcrMhvuU", "HVnmMxH1", "r5.5.1", "2026-09-01T00:00:00Z", h.jars["complementary"], nil, []string{"iris"})}
		case "YL57xq9U":
			return []map[string]any{versionOf("yl57v113", "YL57xq9U", "1.11.3+mc26.2", "2026-09-01T00:00:00Z", h.jars["irisshaders"], nil)}
		case "8oi3bsk5":
			return []map[string]any{versionTagged("urbokcOc", "8oi3bsk5", "2.6.4", "2026-09-01T00:00:00Z", h.jars["terralith"], nil, []string{"datapack"})}
		case "AutoSlb1":
			return []map[string]any{versionTagged("AutoSlv1", "AutoSlb1", "1.0", "2026-09-01T00:00:00Z", h.jars["autoslabs"], nil, []string{"datapack"})}
		}
		if mp, ok := h.modrinthPacks[projectID]; ok {
			list := []map[string]any{}
			for _, v := range mp.versions {
				loaders := v.loaders
				if loaders == nil {
					loaders = []string{"fabric"}
				}
				list = append(list, versionTagged(v.id, projectID, v.number, v.published, v.archive, nil, loaders))
			}
			return list
		}
		return nil
	}
	mux.HandleFunc("/modrinth/version/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/modrinth/version/")
		for _, projectID := range append([]string{"AANobbMI", "P7dR8mSH", "50dA9Sha", "HVnmMxH1", "YL57xq9U", "8oi3bsk5", "AutoSlb1"}, slices.Collect(maps.Keys(h.modrinthPacks))...) {
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
		for _, projectID := range []string{"AANobbMI", "P7dR8mSH", "50dA9Sha", "HVnmMxH1", "YL57xq9U", "8oi3bsk5", "AutoSlb1"} {
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
	mux.HandleFunc("/modrinth/version_files", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hashes    []string `json:"hashes"`
			Algorithm string   `json:"algorithm"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		h.modrinthBatches++
		found := map[string]any{}
		for _, hash := range body.Hashes {
			for _, projectID := range []string{"AANobbMI", "P7dR8mSH", "50dA9Sha", "HVnmMxH1", "YL57xq9U", "8oi3bsk5", "AutoSlb1"} {
				for _, v := range versions(projectID) {
					for _, jar := range h.jars {
						sum := jar.sha1
						if body.Algorithm == "sha512" {
							sum = jar.sha512
						}
						if sum == hash && v["files"].([]map[string]any)[0]["filename"] == jar.filename {
							found[hash] = v
						}
					}
				}
			}
			for projectID := range h.modrinthPacks {
				for _, v := range versions(projectID) {
					if v["files"].([]map[string]any)[0]["hashes"].(map[string]string)[body.Algorithm] == hash {
						found[hash] = v
					}
				}
			}
		}
		writeJSON(w, found)
	})
	mux.HandleFunc("/modrinth/projects", func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		_ = json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids)
		h.modrinthBatches++
		found := []map[string]any{}
		for _, id := range ids {
			if p, ok := projects[id]; ok {
				found = append(found, p)
			}
		}
		writeJSON(w, found)
	})
	mux.HandleFunc("/modrinth/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/modrinth/project/")
		if project, version, ok := strings.Cut(rest, "/version/"); ok {
			id := project
			if p, known := projects[project]; known {
				id = p["id"].(string)
			}
			for packID, mp := range h.modrinthPacks {
				if project == mp.slug {
					id = packID
				}
			}
			for _, v := range versions(id) {
				if v["id"] == version || v["version_number"] == version {
					writeJSON(w, v)
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(rest, "/version") {
			projectID := strings.TrimSuffix(rest, "/version")
			if list := versions(projectID); list != nil {
				var loaders []string
				if _, isPack := h.modrinthPacks[projectID]; isPack && json.Unmarshal([]byte(r.URL.Query().Get("loaders")), &loaders) == nil {
					list = slices.DeleteFunc(list, func(v map[string]any) bool {
						return !slices.ContainsFunc(v["loaders"].([]string), func(l string) bool { return slices.Contains(loaders, l) })
					})
				}
				writeJSON(w, list)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		p, ok := projects[rest]
		for id, mp := range h.modrinthPacks {
			if !ok && (rest == id || rest == mp.slug) {
				p, ok = map[string]any{"id": id, "slug": mp.slug, "title": mp.slug, "client_side": "required", "server_side": "required", "project_type": "modpack"}, true
			}
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, p)
	})
	mux.HandleFunc("/modrinth/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		hits := []map[string]any{}
		for _, slug := range []string{"fabric-api", "sodium", "fresh-animations", "complementary-reimagined", "irisshaders"} {
			p := projects[slug]
			kind, _ := p["project_type"].(string)
			if kind == "" {
				kind = "mod"
			}
			if facets := q.Get("facets"); facets != "" && facets != `[["project_type:`+kind+`"]]` {
				continue
			}
			if !matchesQuery(q.Get("query"), slug, p["title"].(string)) {
				continue
			}
			hit := maps.Clone(p)
			hit["project_id"], hit["project_type"], hit["downloads"] = p["id"], kind, searchDownloads[slug]
			hits = append(hits, hit)
		}
		if limit > 0 && len(hits) > limit {
			hits = hits[:limit]
		}
		writeJSON(w, map[string]any{"hits": hits, "total_hits": len(hits)})
	})
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, r *http.Request) {
		if h.cdnDown[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		if h.cdnDrop[r.URL.Path] {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		for _, jar := range h.jars {
			if strings.HasSuffix(r.URL.Path, jar.filename) {
				h.hit(&h.cdnHits)
				if h.cdnCut[r.URL.Path] {
					w.Header().Set("Content-Length", strconv.Itoa(len(jar.data)))
					w.Write(jar.data[:len(jar.data)/2])
					return
				}
				w.Write(jar.data)
				return
			}
		}
		http.NotFound(w, r)
	})
	h.mojang = map[string]string{}
	mux.HandleFunc("/mojang/profiles/minecraft", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.mojangHits)
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
	mux.HandleFunc("/gdl/", func(w http.ResponseWriter, r *http.Request) {
		listed := map[string]map[string][]string{
			"fabric":   {"${gdlauncher.gameVersion}": {"0.17.3"}},
			"quilt":    {"${gdlauncher.gameVersion}": {"0.30.1"}, "26.2": {}},
			"neoforge": {"26.2": {"26.2.0.81", "26.2.0.82"}},
			"forge":    {"26.2": {"26.2-65.1.3"}},
		}
		loader, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/gdl/"), "/")
		if rest != "v2/manifest.json" || listed[loader] == nil {
			http.NotFound(w, r)
			return
		}
		var games []map[string]any
		for game, ids := range listed[loader] {
			loaders := []map[string]string{}
			for _, id := range ids {
				loaders = append(loaders, map[string]string{"id": id})
			}
			games = append(games, map[string]any{"id": game, "loaders": loaders})
		}
		writeJSON(w, map[string]any{"gameVersions": games})
	})
	mux.HandleFunc("/session/session/minecraft/profile/", func(w http.ResponseWriter, r *http.Request) {
		h.hit(&h.mojangHits)
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
	h.msa = h.fakeSignIn(mux)
	h.server = httptest.NewTLSServer(mux)
	base = h.server.URL
	library := func(name, path string) map[string]any {
		jar, ok := h.neoLibs[path]
		if !ok {
			jar = h.forgeLibs[path]
		}
		return map[string]any{"name": name, "downloads": map[string]any{"artifact": map[string]any{"path": path, "url": base + "/cdn/" + path, "sha1": jar.sha1}}}
	}
	profile, _ := json.Marshal(map[string]any{"minecraft": "26.2", "libraries": []any{
		library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
		map[string]any{"name": "net.neoforged:bundled:1.0", "downloads": map[string]any{"artifact": map[string]any{"path": "net/neoforged/bundled/1.0/bundled-1.0.jar", "url": ""}}},
	}})
	version, _ := json.Marshal(map[string]any{
		"id": "neoforge-26.2.0.87", "inheritsFrom": "26.2", "type": "release",
		"mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
		"arguments": map[string]any{"game": []any{"--launchTarget", "forgeclient"}, "jvm": []any{"-DlibraryDirectory=${library_directory}"}},
		"libraries": []any{
			library("net.neoforged:neoforge:26.2.0.87:universal", "net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar"),
			library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
		},
	})
	h.neoInstaller = makeJarFiles(t, "neoforge-installer", "neoforge-26.2.0.87-installer.jar", map[string]string{"install_profile.json": string(profile), "version.json": string(version)})
	forgeProfile, _ := json.Marshal(map[string]any{"minecraft": "26.2", "libraries": []any{
		library("org.ow2.asm:asm:9.10.1", "org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"),
	}})
	// The client jar has no URL: the installer's processors generate it, the way Forge's own does.
	forgeClient := "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-client.jar"
	forgeVersion, _ := json.Marshal(map[string]any{
		"id": "26.2-forge-65.1.3", "inheritsFrom": "26.2", "type": "release",
		"mainClass": "net.minecraftforge.bootstrap.ForgeBootstrap",
		"arguments": map[string]any{"game": []any{"--launchTarget", "forge_client"}},
		"libraries": []any{
			library("net.minecraftforge:forge:26.2-65.1.3:universal", "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar"),
			map[string]any{"name": "net.minecraftforge:forge:26.2-65.1.3:client", "downloads": map[string]any{"artifact": map[string]any{
				"path": forgeClient, "url": "", "sha1": sha1Hex([]byte(fakeForgeClient)), "size": len(fakeForgeClient),
			}}},
		},
	})
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
			installer: h.neoInstaller, version: "26.2.0.87", versionID: "neoforge-26.2.0.87", instanceKey: "NeoForge", versionJSON: version,
			libs: append(slices.Sorted(maps.Keys(h.neoLibs)), vanillaLib("neoforge")),
		},
		"forge": {
			installer: h.forgeInstaller, version: "65.1.3", versionID: "26.2-forge-65.1.3", instanceKey: "forge", versionJSON: forgeVersion,
			generated: map[string]string{forgeClient: fakeForgeClient},
			libs:      []string{"org/ow2/asm/asm/9.10.1/asm-9.10.1.jar", "net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar", vanillaLib("forge")},
		},
	}
	h.fakeRows(t, base)
	t.Cleanup(h.server.Close)
	return h
}

func fakeVersions(ids ...string) []loader.Version {
	versions := make([]loader.Version, len(ids))
	for i, id := range ids {
		versions[i] = loader.Version{Version: id, Stable: !strings.Contains(id, "-")}
	}
	return versions
}

// fakeRows replaces the loader table with a fake row per loader for the test, each answering
// what the CLI's slices ask of it from the cdn: versions, a profile or an installer, and a server.
func (h *harness) fakeRows(t *testing.T, base string) {
	t.Helper()
	h.jars["fabric-loader"], h.jars["quilt-loader"], h.jars["mixin"] = h.fabricLoader, h.quiltLoader, h.mixin
	h.jars["fabric-server-launch"], h.jars["quilt-server-launch"] = h.serverJar, h.quiltLaunch
	h.jars["neoforge-installer"], h.jars["forge-installer"] = h.neoInstaller, h.forgeInstaller
	for path, jar := range h.neoLibs {
		h.jars[path] = jar
	}
	for path, jar := range h.forgeLibs {
		h.jars[path] = jar
	}
	profile := func(id, mainClass, library string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{
			"id": id, "inheritsFrom": "26.2", "type": "release", "mainClass": mainClass,
			"libraries": []map[string]any{{"name": library, "url": base + "/cdn/"}},
		})
		return raw
	}
	asm := h.neoLibs["org/ow2/asm/asm/9.10.1/asm-9.10.1.jar"]
	fakes := []loader.Fake{
		{
			Name: "fabric", Versions: fakeVersions("0.18.0-beta.1", "0.17.3", "0.17.2"),
			Profile:      profile("fabric-loader-0.17.3-26.2", "net.fabricmc.loader.impl.launch.knot.KnotClient", "net.fabricmc:fabric-loader:0.17.3"),
			EnsureServer: h.fakeServer("1.1.2", h.serverJar, nil),
		},
		{
			Name: "quilt", Versions: fakeVersions("0.20.0-beta.9", "0.30.1", "0.31.0-beta.4", "0.30.0"),
			Profile:      profile("quilt-loader-0.30.1-26.2", "org.quiltmc.loader.impl.launch.knot.KnotClient", "org.quiltmc:quilt-loader:0.30.1"),
			ProvidesJar:  base + "/cdn/org/quiltmc/quilt-loader/0.30.1/" + h.quiltLoader.filename,
			EnsureServer: h.fakeServer("", h.quiltLaunch, map[string]fakeJar{"org.quiltmc:quilt-loader:0.30.1": h.quiltLoader, "net.fabricmc:sponge-mixin:0.17.3": h.mixin}),
		},
		{
			Name: "neoforge", Versions: fakeVersions("26.2.0.56-beta", "26.2.0.87"),
			Installer:    base + "/cdn/net/neoforged/neoforge/26.2.0.87/" + h.neoInstaller.filename,
			EnsureServer: h.fakeServer("", h.neoInstaller, map[string]fakeJar{"net.neoforged:neoforge:26.2.0.87:universal": h.neoLibs["net/neoforged/neoforge/26.2.0.87/neoforge-26.2.0.87-universal.jar"], "org.ow2.asm:asm:9.10.1": asm}),
		},
		{
			Name: "forge", Versions: fakeVersions("65.0.9", "65.1.3"),
			Installer:    base + "/cdn/net/minecraftforge/forge/26.2-65.1.3/" + h.forgeInstaller.filename,
			EnsureServer: h.fakeServer("", h.forgeInstaller, map[string]fakeJar{"net.minecraftforge:forge:26.2-65.1.3:universal": h.forgeLibs["net/minecraftforge/forge/26.2-65.1.3/forge-26.2-65.1.3-universal.jar"], "org.ow2.asm:asm:9.10.1": asm}),
		},
	}
	rows := make([]loader.Loader, len(fakes))
	for i, f := range fakes {
		rows[i] = f.Row()
	}
	real := loader.All
	loader.All = rows
	t.Cleanup(func() { loader.All = real })
}

// fakeServer is a fake row's server install: it locks jar as loader.server with libs under it,
// every one on the cdn, and downloads only what the cache lacks.
func (h *harness) fakeServer(installer string, jar fakeJar, libs map[string]fakeJar) func(context.Context, *loader.Remote, *lock.Lock) (loader.ServerResult, error) {
	return func(ctx context.Context, r *loader.Remote, lk *lock.Lock) (loader.ServerResult, error) {
		var res loader.ServerResult
		s := lk.Loader.Server
		if s == nil {
			s = &lock.ServerJar{Installer: installer, URL: h.server.URL + "/cdn/" + jar.filename, Sha512: jar.sha512}
			if len(libs) > 0 {
				s.Libraries = map[string]lock.Download{}
			}
			for name, lib := range libs {
				path, err := loader.MavenPath(name)
				if err != nil {
					return res, err
				}
				s.Libraries[name] = lock.Download{URL: h.server.URL + "/cdn/" + path, Sha512: lib.sha512}
			}
			lk.Loader.Server = s
			res.ChangedLock = true
		}
		if s.URL == "" {
			s.URL = h.server.URL + "/cdn/" + jar.filename
			res.ChangedLock = true
		}
		downloads := append([]lock.Download{{URL: s.URL, Sha512: s.Sha512}}, slices.Collect(maps.Values(s.Libraries))...)
		for _, dl := range downloads {
			if r.Cache.Has(dl.Sha512) {
				continue
			}
			r.Log("downloading the %s server launcher %s", lk.Loader.Type, lk.Loader.Version)
			if _, err := r.Cache.Ensure(ctx, r.Fetch, dl.URL, dl.Sha512); err != nil {
				return res, err
			}
			res.WasFetched = true
		}
		return res, nil
	}
}

// matchesQuery is how the fake providers search: every word of the query
// somewhere in the project's slug or title.
func matchesQuery(query, slug, title string) bool {
	hay := strings.ToLower(slug + " " + title)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}

// searchDownloads ranks the fake's projects the way a provider's search does,
// most downloaded first.
var searchDownloads = map[string]int64{
	"fabric-api":               900_000_000,
	"sodium":                   228_124_617,
	"fresh-animations":         5_000_000,
	"complementary-reimagined": 3_000,
	"irisshaders":              70_000_000,
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *harness) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := h.newApp(&stdout, &stderr)
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	code := a.run(ctx, args)
	return code, stdout.String(), stderr.String()
}

// eulaIn is whether the config file at path accepts the Minecraft EULA; a config a test hasn't
// written accepts nothing.
func eulaIn(path string) bool {
	cfg, err := config.LoadFile(path)
	return err == nil && cfg.EULA
}

func releaseAgeIn(path string) time.Duration {
	cfg, _ := config.LoadFile(path)
	return cfg.Security.ReleaseAge()
}

// newApp is what every run in a test is built from: the harness's own directories and stdin, and
// every client pointed at its fake server.
func (h *harness) newApp(stdout, stderr io.Writer) *app {
	a := newApp(stdout, stderr)
	a.dir = h.dir
	a.configPath = h.config
	a.home = h.home
	a.stdin = h.stdin
	a.tty = func() bool { return h.tty }
	a.installer = h.fakeInstaller
	a.watcher = h.watch
	if h.build != nil {
		a.build = func() selfupdate.Build { return *h.build }
	}
	if h.exe != "" {
		a.exe = func() (string, error) { return h.exe, nil }
	}
	f := fetch.New("test")
	f.HTTP = fetchtest.Routed(h.server, append(slices.Clone(packarchive.MrpackHosts), "edge.forgecdn.net")...)
	piston := mojang.NewPiston(f)
	piston.ManifestURL = h.server.URL + "/piston/manifest.json"
	mr := modrinth.New(f)
	mr.BaseURL = h.server.URL + "/modrinth"
	runtimes := mojang.NewRuntimes(f)
	runtimes.IndexURL = h.server.URL + "/jrt/all.json"
	profiles := mojang.NewProfiles(f)
	profiles.APIURL = h.server.URL + "/mojang"
	profiles.SessionURL = h.server.URL + "/session"
	players := player.NewResolver(profiles)
	key := curseForgeTestKey
	if h.noCurseForge {
		key = ""
	}
	cf := curseforge.New(f, key)
	cf.BaseURL = h.server.URL + "/curseforge"
	providers := provider.Providers{mr.Name(): mr, cf.Name(): cf}
	c := &cache.Cache{Dir: h.cache}
	loaders := &loader.Remote{Fetch: f, Cache: c, Log: a.progress, RunInstaller: a.installer}
	a.d = a.newDeps(&env.Env{
		Fetch:         f,
		Cache:         c,
		Providers:     providers,
		Loaders:       loaders,
		Piston:        piston,
		Runtimes:      runtimes,
		Players:       players,
		EULA:          eulaIn(h.config),
		MinReleaseAge: releaseAgeIn(h.config),
	})
	if !h.now.IsZero() {
		a.d.Now = func() time.Time { return h.now }
	}
	a.d.metaURLs = map[string]string{launcher.GDLauncherMetaURL: h.server.URL + "/gdl"}
	a.d.signin = h.msa.signIn(f, h.server.URL)
	a.d.resources = h.server.URL + "/resources"
	return a
}

func (h *harness) mustRunStderr(t *testing.T, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr := h.run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout, stderr
}

// runStderr runs a command expected to fail and returns its code and stderr.
func (h *harness) runStderr(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, _, stderr := h.run(t, args...)
	if code == 0 {
		t.Fatalf("%v succeeded", args)
	}
	return code, stderr
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

	stdout := h.mustRun(t, "create", "--loader", "fabric")
	if !strings.Contains(stdout, "Created shulker.json (Minecraft 26.2, Fabric 0.17.3, Java 25)") {
		t.Fatalf("init output: %s", stdout)
	}
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	if m["minecraft"] != "26.2" || m["name"] != filepath.Base(h.dir) {
		t.Fatalf("manifest: %v", m)
	}
	if code, _, _ := h.run(t, "create", "--loader", "fabric"); code == 0 {
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
			Size       int64    `json:"size"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if l.Mods["fabric-api"].Side != "both" || l.Mods["fabric-api"].RequiredBy[0] != "sodium" || len(l.Mods["sodium"].RequiredBy) != 0 || l.Mods["sodium"].Size != int64(len(h.jars["sodium"].data)) {
		t.Fatalf("lock mods: %+v", l.Mods)
	}
	h.readJSON(t, "shulker.json", &m)
	if mods := m["requires"].(map[string]any); len(mods) != 1 || len(mods["sodium"].(map[string]any)) != 0 {
		t.Fatalf("manifest mods: %v", m["requires"])
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
	if !strings.Contains(stdout, "Built client (4 written") {
		t.Fatalf("install output: %s", stdout)
	}
	buildDir := filepath.Join(h.dir, "build", "client")
	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "mods/" + h.jars["fabric-api"].filename, "options.txt", filepath.Join(instance.Dir, instance.StateFile)} {
		if _, err := os.Stat(filepath.Join(buildDir, rel)); err != nil {
			t.Fatal(err)
		}
	}
	var state instance.State
	h.readJSON(t, "build/client/.shulker/state.json", &state)
	if state.Side != "client" || len(state.Files) != 4 || state.Schema != schema.URL(schema.State) {
		t.Fatalf("state: %+v", state)
	}
	if err := schema.Validate(schema.State, []byte(readFile(t, filepath.Join(buildDir, instance.Dir, instance.StateFile)))); err != nil {
		t.Fatalf("state.json against its schema: %v", err)
	}

	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "Built client (4 unchanged)") {
		t.Fatalf("rebuild output: %s", stdout)
	}

	options := filepath.Join(buildDir, "options.txt")
	if err := os.WriteFile(options, []byte("renderDistance:8\nlang:en_us\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 kept") || !strings.Contains(stdout, "kept: options.txt") {
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
	if !strings.Contains(stdout, "Built client (4 unchanged)") {
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
	h.mustRun(t, "create", "--loader", "fabric")
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
	h.mustRun(t, "create", "--loader", "fabric")
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
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "shulker.lock is out of date with shulker.json (sodium: channel release -> beta); run `shulker lock`") {
		t.Fatalf("code=%d env=%+v", code, env)
	}
	code, stdout, _ = h.run(t, "export", "mrpack", "--version", "1.0.0", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || !env.LockStale || env.Error.Code != "lock-stale" || len(env.Error.Items) != 1 || env.Error.Items[0] != "sodium: channel release -> beta" {
		t.Fatalf("export must refuse a stale lock: code=%d env=%+v", code, env)
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "shulker.lock is out of date") {
		t.Fatalf("install warns about the stale lock in the envelope: %+v", env)
	}
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.Mods["sodium"].Channel != "beta" {
		t.Fatalf("lock records the new channel: %+v", l.Mods["sodium"])
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "build", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("build after lock: %+v", env)
	}
}

func TestAddLocksHandAddedMods(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"sodium": map[string]any{}}
	})
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "add", "fabric-api", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	added := env.Data.(map[string]any)["added"].([]any)
	if env.LockStale || len(added) != 2 || added[1].(map[string]any)["id"] != "sodium" {
		t.Fatalf("add after a hand-added mod: %+v", env)
	}
	if l := h.readLock(t); l.Mods["sodium"].Channel != "release" || len(l.Mods["fabric-api"].RequiredBy) != 1 {
		t.Fatalf("lock: %+v", l.Mods)
	}
	if _, stderr := h.mustRunStderr(t, "build"); strings.Contains(stderr, "out of date") {
		t.Fatalf("build after add: %s", stderr)
	}
}

func TestLockOnlyRepicksWhatChanged(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium", "fabric-api")
	h.newer, h.newerAPI = true, true
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fabric-api"] = map[string]any{"side": "server"}
	})
	code, stdout, _ := h.run(t, "install", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || !strings.Contains(strings.Join(env.Warnings, "\n"), "(fabric-api: side both -> server)") {
		t.Fatalf("stale warning: code=%d env=%+v", code, env)
	}
	code, stdout, _ = h.run(t, "lock", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	updated, _ := env.Data.(map[string]any)["updated"].([]any)
	if code != 0 || len(updated) != 1 || updated[0].(map[string]any)["fromSide"] != "both" || updated[0].(map[string]any)["toSide"] != "server" {
		t.Fatalf("lock must report the side change: %s", stdout)
	}
	if l := h.readLock(t); l.Mods["sodium"].VersionNumber != "1.0.0+mc26.2" || l.Mods["fabric-api"].Side != "server" || l.Mods["fabric-api"].VersionNumber != "1.0.0+mc26.2" {
		t.Fatalf("a side change must move no version: %+v", l.Mods)
	}
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fabric-api"] = map[string]any{"side": "client"}
	})
	if out := h.mustRun(t, "lock"); !strings.Contains(out, "~ fabric-api ") || !strings.Contains(out, "(now client only)") {
		t.Fatalf("lock after a second side change: %s", out)
	}
	if out := h.mustRun(t, "lock"); !strings.Contains(out, "Already up to date") {
		t.Fatalf("second lock: %s", out)
	}
	for _, args := range [][]string{{"add", "sodium", "--pin", "nope"}, {"pin", "sodium", "nope"}} {
		code, stdout, _ := h.run(t, append(args, "--json")...)
		if code != 1 || !strings.Contains(stdout, `"version-not-found"`) || !strings.Contains(stdout, "modrinth.com/mod/sodium/versions") {
			t.Fatalf("%v with an unknown version id: %d %s", args, code, stdout)
		}
	}
}

func TestNoCompatibleVersionExampleFitsTheCommand(t *testing.T) {
	h := newHarness(t)
	h.sodiumBeta = true
	h.mustRun(t, "create", "--loader", "fabric")
	if _, stderr := h.runStderr(t, "add", "sodium"); !strings.Contains(stderr, "$ shulker add sodium --channel beta") {
		t.Fatalf("add example:\n%s", stderr)
	}
	h.mustRun(t, "add", "sodium", "--channel", "beta")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["sodium"] = map[string]any{"side": "client"}
	})
	for _, args := range [][]string{{"lock"}, {"pin", "sodium"}, {"update", "sodium"}, {"add", "irisshaders"}} {
		_, stderr := h.runStderr(t, args...)
		if !strings.Contains(stderr, "no-compatible-version") || !strings.Contains(stderr, "$ shulker set requires.sodium.channel beta") || strings.Contains(stderr, "--channel") {
			t.Fatalf("%v example:\n%s", args, stderr)
		}
	}
}

func TestPinningAnUnknownPackVersionLinksItsVersions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	code, stdout, _ := h.run(t, "add", "fresh-animations", "--pin", "nope", "--json")
	if code != 1 || !strings.Contains(stdout, `"version-not-found"`) || !strings.Contains(stdout, "modrinth.com/resourcepack/fresh-animations/versions") {
		t.Fatalf("add a pack pinned to an unknown version: %d %s", code, stdout)
	}
}

func TestLockRecreatesADeletedLock(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	if err := os.Remove(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := h.run(t, "build", "--json"); code == 0 || !strings.Contains(stdout, `"code": "lock-not-found"`) {
		t.Fatalf("build without a lock: code=%d %s", code, stdout)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	data := env.Data.(map[string]any)
	platform := data["platform"].([]any)
	if len(platform) != 2 || platform[0].(map[string]any)["to"] != "26.2" || len(data["added"].([]any)) != 2 || len(data["reresolved"].([]any)) != 2 {
		t.Fatalf("lock from scratch: %+v", data)
	}
	if l := h.readLock(t); l.Minecraft != "26.2" || len(l.Mods) != 2 {
		t.Fatalf("recreated lock: %+v", l)
	}
	if err := os.Remove(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "lock"); !strings.Contains(stdout, "Created shulker.lock") || strings.Contains(stdout, "Re-resolved") {
		t.Fatalf("a lock from scratch says it created one: %s", stdout)
	}
}

func TestRemoveValidatesBeforeSaving(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "create", "--loader", "fabric")
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
	h.mustRun(t, "create", "--loader", "fabric")
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
	code, stdout, _ := h.run(t, "create", "--loader", "rift", "--json")
	if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" || len(e.Candidates) != 5 {
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
	h.mustRun(t, "create", "--loader", "fabric")

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
	if !strings.Contains(env.Error.Message, `Ignore: shulker ignore sodium fabric-api --rule depends --declared ">=2.0.0" --note "why this is safe"`) {
		t.Fatalf("message should print the ignore command: %s", env.Error.Message)
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
	if !strings.Contains(stdout, "Built client (4 written") {
		t.Fatalf("install: %s", stdout)
	}
}

func TestUpdateOutdatedAndPin(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")

	if out := h.mustRun(t, "outdated"); !strings.Contains(out, "Everything is up to date") {
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
		} `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if m.Mods["sodium"].Pin != "QANobbMI" {
		t.Fatalf("manifest pin: %+v", m.Mods)
	}
	if out := h.mustRun(t, "outdated"); !strings.Contains(out, "sodium 1.0.0+mc26.2 → 1.1.0+mc26.2 (pinned)") {
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

func TestDiffAndPull(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
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
	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "No changes in client (the build directory)") {
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
	if d := diffs["options.txt"]; !strings.Contains(d, "-joinedFirstServer:true\n+joinedFirstServer:false\n") || strings.Contains(d, "foo") {
		t.Fatalf("options diff should cover managed keys only: %s", d)
	}
	if d := diffs["config/plain.txt"]; !strings.Contains(d, "@@ -1 +1 @@\n-a=1\n+a=2\n") {
		t.Fatalf("plain diff: %s", d)
	}

	stdout = h.mustRun(t, "pull")
	for _, line := range []string{
		"Pulled client (1 file, 1 key written to shulker.json, 2 skipped)",
		"pulled: config/plain.txt → overrides/config/plain.txt",
		"set: options.txt joinedFirstServer=false",
		"skipped: config/new.txt (not written by shulker; name it to adopt it)",
		"skipped: config/tpl.txt (rendered from template overrides/config/tpl.txt.tmpl)",
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

	if code, _, stderr := h.run(t, "pull", "config/missing.txt"); code == 0 || !strings.Contains(stderr, "config/missing.txt is not in ") || !strings.Contains(stderr, "(file-not-found)") {
		t.Fatalf("pull of a file that isn't there should fail with file-not-found: %d %s", code, stderr)
	}
	stdout = h.mustRun(t, "pull", "config/new.txt")
	if !strings.Contains(stdout, "pulled: config/new.txt → overrides/config/new.txt") {
		t.Fatalf("named untracked file should be adopted: %s", stdout)
	}
	if data, _ := os.ReadFile(filepath.Join(overrides, "new.txt")); string(data) != "hand\n" {
		t.Fatalf("adopted override: %q", data)
	}

	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 written, 5 unchanged, 1 kept") || !strings.Contains(stdout, "kept: config/tpl.txt") {
		t.Fatalf("build after pull: %s", stdout)
	}
	if data, _ := os.ReadFile(filepath.Join(buildDir, "options.txt")); !strings.Contains(string(data), "joinedFirstServer:false\n") || !strings.Contains(string(data), "foo:bar\n") {
		t.Fatalf("options after pull: %q", data)
	}

	write(h.dir, "overrides/config/plain.txt", "a=3\n")
	write(buildDir, "config/plain.txt", "a=4\n")
	stdout = h.mustRun(t, "diff")
	if !strings.Contains(stdout, "~ config/plain.txt (edited on both sides)\n") || !strings.Contains(stdout, "    -a=3\n    +a=4\n") {
		t.Fatalf("conflict diff: %s", stdout)
	}
	h.mustRun(t, "pull", "config/plain.txt")
	if data, _ := os.ReadFile(filepath.Join(overrides, "plain.txt")); string(data) != "a=4\n" {
		t.Fatalf("pulled conflict should take the build file: %q", data)
	}
	if stdout = h.mustRun(t, "build"); !strings.Contains(stdout, "Built client (6 unchanged, 1 kept)") {
		t.Fatalf("build after conflict pull: %s", stdout)
	}
}

// fakeForgeClient is the jar the fake Forge installer's processors generate for a client.
const fakeForgeClient = "forge client"

// fakeLoaderInstall is what the harness pretends each loader's installer ships and generates.
type fakeLoaderInstall struct {
	installer   fakeJar
	version     string
	versionID   string
	instanceKey string
	libs        []string
	versionJSON []byte
	// generated is what a client install's processors write under libraries/, by path.
	generated map[string]string
}

// installerLoader is the loader whose installer jar is at path, since the fake rows share a
// client flag.
func (h *harness) installerLoader(path string) (loader.Loader, fakeLoaderInstall, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return loader.Loader{}, fakeLoaderInstall{}, err
	}
	for name, fake := range h.loaders {
		if string(data) == string(fake.installer.data) {
			l, _ := loader.Lookup(name)
			return l, fake, nil
		}
	}
	return loader.Loader{}, fakeLoaderInstall{}, fmt.Errorf("installer jar %s is not a locked one", path)
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
	l, fake, err := h.installerLoader(jar)
	if err != nil {
		return err
	}
	if args[0] == l.InstallClientFlag {
		return h.fakeClientInstall(args, fake)
	}
	if args[0] != l.InstallServerFlag || len(args) != 3 || args[2] != "--offline" {
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
// installed into the launcher, runs its processors over the vanilla client jar, and, like the real
// one, leaves a launcher profile of its own behind.
func (h *harness) fakeClientInstall(args []string, fake fakeLoaderInstall) error {
	if len(args) != 2 {
		return fmt.Errorf("installer args %v", args)
	}
	dir, id := args[1], fake.versionID
	vanilla := filepath.Join(dir, "versions", "26.2", "26.2.jar")
	if _, err := os.Stat(vanilla); err != nil {
		h.installerFetchedClient = true
		if err := os.MkdirAll(filepath.Dir(vanilla), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(vanilla, []byte("client"), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "versions", id), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "versions", id, id+".json"), fake.versionJSON, 0o644); err != nil {
		return err
	}
	generated := map[string]string{"fake/" + id + "/client-patched.jar": "patched"}
	maps.Copy(generated, fake.generated)
	for rel, content := range generated {
		path := filepath.Join(dir, "libraries", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
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
	profiles[fake.instanceKey] = json.RawMessage(`{"name":"` + fake.instanceKey + `","type":"custom","lastVersionId":"` + id + `"}`)
	if top["profiles"], err = json.MarshalIndent(profiles, "", "  "); err != nil {
		return err
	}
	written, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, written, 0o644)
}
