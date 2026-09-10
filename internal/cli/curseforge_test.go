package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
)

const curseForgeTestKey = "test-key"

type cfMod struct {
	id    int
	slug  string
	files []cfFile
}

type cfFile struct {
	id        int
	jar       fakeJar
	url       string
	date      string
	channel   int
	deps      []int
	hidden    bool
	forbidden bool
}

func (h *harness) registerCurseForge(t *testing.T, mux *http.ServeMux, base func() string) {
	jei := makeJar(t, "jei", "jei-26.2-fabric-1.0.0.jar", "*")
	h.jars["jei"], h.jars["jei-next"] = jei, makeJarVersion(t, "jei", "jei-26.2-fabric-1.1.0.jar", "*", "1.1.0", `"depends":{"fabricloader":">=0.17"}`)
	h.jars["nodist"] = makeJar(t, "nodist", "nodist-1.0.0.jar", "client")
	h.jars["locked"] = makeJar(t, "locked", "locked-1.0.0.jar", "*")
	h.jars["stale"] = makeJarVersion(t, "jei", "jei-26.1-fabric-0.9.0.jar", "*", "0.9.0", `"depends":{"fabricloader":">=0.17"}`)
	h.cfMods = map[int]*cfMod{
		238222: {id: 238222, slug: "jei", files: []cfFile{
			{id: 5000001, jar: jei, date: "2026-09-01T00:00:00Z", channel: 1, deps: []int{306612}},
			{id: 5000000, jar: h.jars["stale"], date: "2026-08-01T00:00:00Z", channel: 1, hidden: true},
		}},
		306612: {id: 306612, slug: "fabric-api", files: []cfFile{{id: 5000010, jar: h.jars["fabric-api"], date: "2026-09-01T00:00:00Z", channel: 1}}},
		394468: {id: 394468, slug: "sodium", files: []cfFile{{id: 5000020, jar: h.jars["sodium"], date: "2026-09-01T00:00:00Z", channel: 1, deps: []int{306612}}}},
		300000: {id: 300000, slug: "nodist", files: []cfFile{{id: 5100001, jar: h.jars["nodist"], date: "2026-09-01T00:00:00Z", channel: 1, url: "null"}}},
		400000: {id: 400000, slug: "locked", files: []cfFile{{id: 5200001, jar: h.jars["locked"], date: "2026-09-01T00:00:00Z", channel: 1, forbidden: true}}},
	}
	fileJSON := func(f cfFile, modID int) map[string]any {
		var url any = base() + "/cfcdn/" + f.jar.filename
		if f.url == "null" {
			url = nil
		} else if f.forbidden {
			url = base() + "/cfcdn/forbidden/" + f.jar.filename
		}
		deps := []map[string]any{}
		for _, d := range f.deps {
			deps = append(deps, map[string]any{"modId": d, "relationType": 3})
		}
		return map[string]any{
			"id": f.id, "modId": modID, "displayName": strings.TrimSuffix(f.jar.filename, ".jar"), "fileName": f.jar.filename,
			"releaseType": f.channel, "fileDate": f.date, "downloadUrl": url, "isAvailable": !f.hidden,
			"gameVersions": []string{"26.2", "Fabric"},
			"hashes":       []map[string]any{{"value": f.jar.sha1, "algo": 1}, {"value": "00", "algo": 2}},
			"dependencies": deps,
		}
	}
	modJSON := func(m *cfMod) map[string]any {
		return map[string]any{"id": m.id, "name": strings.ToUpper(m.slug), "slug": m.slug, "links": map[string]any{"websiteUrl": "https://www.curseforge.com/minecraft/mc-mods/" + m.slug}}
	}
	allFiles := func(m *cfMod) []cfFile {
		files := m.files
		if m.id == 238222 && h.newer {
			files = append([]cfFile{{id: 5000002, jar: h.jars["jei-next"], date: "2026-09-05T00:00:00Z", channel: 1, deps: []int{306612}}}, files...)
		}
		return files
	}
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		h.cfHits++
		if r.Header.Get("X-Api-Key") != curseForgeTestKey {
			w.WriteHeader(http.StatusForbidden)
			return false
		}
		return true
	}
	mux.HandleFunc("/curseforge/mods/search", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		if r.URL.Query().Get("gameId") != "432" || r.URL.Query().Get("classId") != "6" {
			http.Error(w, "bad filter", http.StatusBadRequest)
			return
		}
		data := []map[string]any{}
		for _, m := range h.cfMods {
			if strings.HasPrefix(m.slug, r.URL.Query().Get("slug")) {
				data = append(data, modJSON(m))
			}
		}
		writeJSON(w, map[string]any{"data": data})
	})
	mux.HandleFunc("/curseforge/mods/files", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var body struct {
			FileIDs []int `json:"fileIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		data := []map[string]any{}
		for _, m := range h.cfMods {
			for _, f := range allFiles(m) {
				for _, want := range body.FileIDs {
					if f.id == want {
						data = append(data, fileJSON(f, m.id))
					}
				}
			}
		}
		writeJSON(w, map[string]any{"data": data})
	})
	mux.HandleFunc("/curseforge/mods/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/curseforge/mods/"), "/")
		id, _ := strconv.Atoi(parts[0])
		m, ok := h.cfMods[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 {
			writeJSON(w, map[string]any{"data": modJSON(m)})
			return
		}
		if r.URL.Query().Get("modLoaderType") != "4" {
			writeJSON(w, map[string]any{"data": []any{}, "pagination": map[string]int{"totalCount": 0}})
			return
		}
		data := []map[string]any{}
		for _, f := range allFiles(m) {
			data = append(data, fileJSON(f, m.id))
		}
		writeJSON(w, map[string]any{"data": data, "pagination": map[string]int{"index": 0, "resultCount": len(data), "totalCount": len(data)}})
	})
	mux.HandleFunc("/cfcdn/forbidden/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	mux.HandleFunc("/cfcdn/", func(w http.ResponseWriter, r *http.Request) {
		for _, jar := range h.jars {
			if strings.HasSuffix(r.URL.Path, "/"+jar.filename) {
				w.Write(jar.data)
				return
			}
		}
		http.NotFound(w, r)
	})
}

func (h *harness) readLock(t *testing.T) *lock.Lock {
	t.Helper()
	l, err := lock.Load(filepath.Join(h.dir, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (h *harness) readManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(filepath.Join(h.dir, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCurseForgeAddFallsThrough(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")

	stdout := h.mustRun(t, "add", "jei")
	if !strings.Contains(stdout, "+ jei jei-26.2-fabric-1.0.0 (both) with fabric-api") {
		t.Fatalf("add: %s", stdout)
	}
	l := h.readLock(t)
	jei := l.Mods["jei"]
	if jei.Provider != "curseforge" || jei.Project.(json.Number) != "238222" || jei.Version.(json.Number) != "5000001" || jei.Sha512 != h.jars["jei"].sha512 || jei.URL == nil || jei.Page != "" || jei.Side != "both" {
		t.Fatalf("jei lock entry: %+v", jei)
	}
	if dep := l.Mods["fabric-api"]; dep.Provider != "curseforge" || dep.RequiredBy[0] != "jei" {
		t.Fatalf("fabric-api lock entry: %+v", dep)
	}
	m := h.readManifest(t)
	if entry := m.Mods["jei"]; entry.Project.(json.Number) != "238222" || entry.Provider != "curseforge" {
		t.Fatalf("jei manifest entry: %+v", entry)
	}
	h.mustRun(t, "install")

	if stdout := h.mustRun(t, "outdated"); !strings.Contains(stdout, "All mods are up to date") {
		t.Fatalf("outdated before a new file: %s", stdout)
	}
	h.newer = true
	stdout = h.mustRun(t, "outdated")
	if !strings.Contains(stdout, "jei") || !strings.Contains(stdout, "1.1.0") {
		t.Fatalf("outdated: %s", stdout)
	}
	h.mustRun(t, "update")
	if v := h.readLock(t).Mods["jei"].Version.(json.Number); v != "5000002" {
		t.Fatalf("update left version %s", v)
	}
	h.mustRun(t, "pin", "jei", "5000001")
	if pin := h.readManifest(t).Mods["jei"].Pin.(json.Number); pin != "5000001" {
		t.Fatalf("pin: %v", pin)
	}
	if v := h.readLock(t).Mods["jei"].Version.(json.Number); v != "5000001" {
		t.Fatalf("pin left version %s", v)
	}

	code, stdout, _ := h.run(t, "--json", "add", "nothing-anywhere")
	if e := failureCode(t, stdout); code == 0 || e.Code != "mod-not-found" || !strings.Contains(e.Message, "modrinth or curseforge") {
		t.Fatalf("expected mod-not-found, got %d %s", code, stdout)
	}

	stdout = h.mustRun(t, "add", "394468")
	if !strings.Contains(stdout, "+ sodium") || h.readLock(t).Mods["sodium"].Provider != "curseforge" {
		t.Fatalf("add by id: %s", stdout)
	}
}

func TestCurseForgeAliasAndAbsence(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	h.mustRun(t, "add", "sodium")

	_, stdout, _ := h.run(t, "--json", "add", "sodium", "--provider", "curseforge")
	var env struct {
		Data struct {
			Added []map[string]any `json:"added"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || len(env.Data.Added) != 1 || env.Data.Added[0]["alreadyLocked"] != true || env.Data.Added[0]["switchedFrom"] != "modrinth" {
		t.Fatalf("add via curseforge: %s", stdout)
	}
	sodium := h.readLock(t).Mods["sodium"]
	if sodium.Provider != "curseforge" || sodium.Aliases.Modrinth != "AANobbMI" || sodium.Aliases.CurseForge != 0 {
		t.Fatalf("switched entry: %+v", sodium)
	}
	if by := h.readLock(t).Mods["fabric-api"].RequiredBy; len(by) != 1 || by[0] != "sodium" {
		t.Fatalf("fabric-api requiredBy after switch: %v", by)
	}
	var m struct {
		Mods map[string]map[string]any `json:"mods"`
	}
	h.readJSON(t, "shulker.json", &m)
	if m.Mods["sodium"]["provider"] != "curseforge" || m.Mods["sodium"]["project"] != float64(394468) {
		t.Fatalf("manifest after switch: %v", m.Mods["sodium"])
	}
	stdout = h.mustRun(t, "add", "sodium", "--provider", "modrinth")
	if !strings.HasPrefix(stdout, "~ sodium ") || !strings.Contains(stdout, "curseforge -> modrinth") {
		t.Fatalf("switch back: %s", stdout)
	}
	sodium = h.readLock(t).Mods["sodium"]
	if sodium.Provider != "modrinth" || sodium.Aliases.CurseForge != 394468 || sodium.Aliases.Modrinth != "" {
		t.Fatalf("switched back entry: %+v", sodium)
	}
	if stdout = h.mustRun(t, "add", "sodium", "--provider", "curseforge"); !strings.Contains(stdout, "modrinth -> curseforge") {
		t.Fatalf("second switch: %s", stdout)
	}
	if stdout = h.mustRun(t, "add", "sodium"); !strings.HasPrefix(stdout, "+ sodium ") || h.readLock(t).Mods["sodium"].Provider != "curseforge" {
		t.Fatalf("plain add after switch should keep the curseforge entry: %s", stdout)
	}

	h.noCurseForge = true
	code, stdout, _ := h.run(t, "--json", "add", "jei", "--provider", "curseforge")
	if e := failureCode(t, stdout); code == 0 || e.Code != "provider-unavailable" {
		t.Fatalf("expected provider-unavailable, got %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "--json", "add", "jei")
	if e := failureCode(t, stdout); code == 0 || e.Code != "mod-not-found" || strings.Contains(e.Message, "curseforge") {
		t.Fatalf("expected a modrinth-only miss, got %d %s", code, stdout)
	}
}

func TestCurseForgeManualDownloads(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	if gi, _ := os.ReadFile(filepath.Join(h.dir, ".gitignore")); !strings.Contains(string(gi), "/downloads/") {
		t.Fatalf(".gitignore: %s", gi)
	}

	code, stdout, _ := h.run(t, "--json", "add", "nodist")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "manual-download" || !strings.Contains(e.Message, "https://www.curseforge.com/minecraft/mc-mods/nodist/files/5100001") || !strings.Contains(e.Message, "nodist-1.0.0.jar") {
		t.Fatalf("expected manual-download, got %d %s", code, stdout)
	}

	downloads := filepath.Join(h.dir, "downloads")
	os.MkdirAll(downloads, 0o755)
	os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	stdout = h.mustRun(t, "add", "nodist")
	if !strings.Contains(stdout, "+ nodist nodist-1.0.0 (client)") {
		t.Fatalf("add after drop: %s", stdout)
	}
	nodist := h.readLock(t).Mods["nodist"]
	if nodist.URL != nil || nodist.Page != "https://www.curseforge.com/minecraft/mc-mods/nodist/files/5100001" || nodist.Sha512 != h.jars["nodist"].sha512 {
		t.Fatalf("nodist lock entry: %+v", nodist)
	}

	code, stdout, _ = h.run(t, "--json", "add", "locked")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manual-download" || !strings.Contains(e.Message, "mc-mods/locked/files/5200001") {
		t.Fatalf("expected manual-download after 403, got %d %s", code, stdout)
	}
	os.WriteFile(filepath.Join(downloads, "locked-1.0.0.jar"), h.jars["locked"].data, 0o644)
	h.mustRun(t, "add", "locked")
	if locked := h.readLock(t).Mods["locked"]; locked.URL != nil || !strings.Contains(locked.Page, "mc-mods/locked/files/5200001") {
		t.Fatalf("locked lock entry: %+v", locked)
	}

	os.WriteFile(filepath.Join(downloads, "unrelated.jar"), []byte("not a mod"), 0o644)
	os.RemoveAll(h.cache)
	_, stderr := h.mustRunStderr(t, "install")
	if !strings.Contains(stderr, "downloads/unrelated.jar matches no mod in the lock") {
		t.Fatalf("install warnings: %s", stderr)
	}
	build := filepath.Join(h.dir, "build", "client", "mods")
	for _, name := range []string{"nodist-1.0.0.jar", "locked-1.0.0.jar"} {
		if _, err := os.Stat(filepath.Join(build, name)); err != nil {
			t.Fatalf("%s not built: %v", name, err)
		}
	}

	os.RemoveAll(downloads)
	os.RemoveAll(h.cache)
	code, stdout, _ = h.run(t, "--json", "install")
	e = failureCode(t, stdout)
	if code == 0 || e.Code != "missing-files" || len(e.Candidates) != 2 || !strings.Contains(e.Candidates[0], "mc-mods/locked/files/5200001") || !strings.Contains(e.Candidates[1], "nodist-1.0.0.jar") {
		t.Fatalf("expected missing-files, got %d %s", code, stdout)
	}
}
