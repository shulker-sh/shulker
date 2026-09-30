package cli

import (
	"cmp"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

const curseForgeTestKey = "test-key"

type cfMod struct {
	id        int
	slug      string
	class     int
	downloads int64
	files     []cfFile
}

func (c *cfMod) classID() int {
	if c.class == 0 {
		return 6
	}
	return c.class
}

type cfFile struct {
	id      int
	jar     fakeJar
	url     string
	date    string
	channel int
	deps    []int
	hidden  bool
}

func (h *harness) registerCurseForge(t *testing.T, mux *http.ServeMux, base func() string) {
	jei := makeJar(t, "jei", "jei-26.2-fabric-1.0.0.jar", "*")
	h.jars["jei"], h.jars["jei-next"] = jei, makeJarVersion(t, "jei", "jei-26.2-fabric-1.1.0.jar", "*", "1.1.0", `"depends":{"fabricloader":">=0.17"}`)
	h.jars["nodist"] = makeJar(t, "nodist", "nodist-1.0.0.jar", "client")
	h.jars["cf-fresh-animations"] = makeJarFile(t, "fresh-animations", "FreshAnimations_CF_v1.9.4.zip", "pack.mcmeta", `{"pack":{"pack_format":34,"description":"fresh"}}`)
	h.jars["stale"] = makeJarVersion(t, "jei", "jei-26.1-fabric-0.9.0.jar", "*", "0.9.0", `"depends":{"fabricloader":">=0.17"}`)
	h.cfMods = map[int]*cfMod{
		238222: {id: 238222, slug: "jei", downloads: 300_000_000, files: []cfFile{
			{id: 5000001, jar: jei, date: "2026-09-01T00:00:00Z", channel: 1, deps: []int{306612}},
			{id: 5000000, jar: h.jars["stale"], date: "2026-08-01T00:00:00Z", channel: 1, hidden: true},
		}},
		306612: {id: 306612, slug: "fabric-api", downloads: 200_000_000, files: []cfFile{{id: 5000010, jar: h.jars["fabric-api"], date: "2026-09-01T00:00:00Z", channel: 1}}},
		394468: {id: 394468, slug: "sodium", downloads: 151_434_981, files: []cfFile{{id: 5000020, jar: h.jars["sodium"], date: "2026-09-01T00:00:00Z", channel: 1, deps: []int{306612}}}},
		300000: {id: 300000, slug: "nodist", files: []cfFile{{id: 5100001, jar: h.jars["nodist"], date: "2026-09-01T00:00:00Z", channel: 1, url: "null"}}},
		300002: {id: 300002, slug: "iris-cf", files: []cfFile{{id: 5100002, jar: h.jars["irisshaders"], date: "2026-09-01T00:00:00Z", channel: 1, url: "null"}}},
		600000: {id: 600000, slug: "fresh-animations", class: 12, downloads: 4_000_000, files: []cfFile{{id: 5300001, jar: h.jars["cf-fresh-animations"], date: "2026-09-01T00:00:00Z", channel: 1}}},
	}
	fileJSON := func(f cfFile, m *cfMod) map[string]any {
		var url any = "https://edge.forgecdn.net/cfcdn/" + strconv.Itoa(f.id) + "/" + f.jar.filename
		if f.url == "null" {
			url = nil
		}
		deps := []map[string]any{}
		for _, d := range f.deps {
			deps = append(deps, map[string]any{"modId": d, "relationType": 3})
		}
		// A pack's files carry no loader tag.
		gameVersions := []string{"26.2", "Fabric"}
		if m.classID() == 12 {
			gameVersions = []string{"26.2"}
		}
		return map[string]any{
			"id": f.id, "modId": m.id, "displayName": strings.TrimSuffix(f.jar.filename, ".jar"), "fileName": f.jar.filename,
			"releaseType": f.channel, "fileDate": f.date, "downloadUrl": url, "isAvailable": !f.hidden, "fileLength": len(f.jar.data),
			"gameVersions": gameVersions,
			"hashes":       []map[string]any{{"value": f.jar.sha1, "algo": 1}, {"value": "00", "algo": 2}},
			"dependencies": deps,
		}
	}
	modJSON := func(m *cfMod) map[string]any {
		return map[string]any{
			"id": m.id, "name": strings.ToUpper(m.slug), "slug": m.slug, "classId": m.classID(), "downloadCount": m.downloads,
			"links":   map[string]any{"websiteUrl": "https://www.curseforge.com/minecraft/mc-mods/" + m.slug},
			"authors": []map[string]any{{"name": m.slug + "-dev"}, {"name": "helper"}},
		}
	}
	allFiles := func(m *cfMod) []cfFile {
		files := m.files
		if m.id == 238222 && h.newer {
			files = append([]cfFile{{id: 5000002, jar: h.jars["jei-next"], date: "2026-09-05T00:00:00Z", channel: 1, deps: []int{306612}}}, files...)
		}
		return files
	}
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		h.hit(&h.cfHits)
		if r.Header.Get("X-Api-Key") != curseForgeTestKey {
			w.WriteHeader(http.StatusForbidden)
			return false
		}
		return true
	}
	mux.HandleFunc("/curseforge/fingerprints", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var body struct {
			Fingerprints []uint32 `json:"fingerprints"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, map[string]any{"data": map[string]any{"exactMatches": h.fingerprintMatches(body.Fingerprints)}})
	})
	mux.HandleFunc("/curseforge/mods/search", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		if h.cfSearchFails {
			http.Error(w, "search is down", http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("gameId") != "432" {
			http.Error(w, "bad filter", http.StatusBadRequest)
			return
		}
		// A slug search without a classId spans every class, as the real API does.
		q := r.URL.Query()
		class := q.Get("classId")
		var matched []*cfMod
		for _, m := range h.cfMods {
			if class != "" && class != strconv.Itoa(m.classID()) {
				continue
			}
			if words := q.Get("searchFilter"); words != "" {
				if matchesQuery(words, m.slug, strings.ToUpper(m.slug)) {
					matched = append(matched, m)
				}
				continue
			}
			if strings.HasPrefix(m.slug, q.Get("slug")) {
				matched = append(matched, m)
			}
		}
		slices.SortFunc(matched, func(a, b *cfMod) int { return cmp.Compare(b.downloads, a.downloads) })
		if size, _ := strconv.Atoi(q.Get("pageSize")); size > 0 && len(matched) > size {
			matched = matched[:size]
		}
		data := []map[string]any{}
		for _, m := range matched {
			data = append(data, modJSON(m))
		}
		writeJSON(w, map[string]any{"data": data})
	})
	mux.HandleFunc("/curseforge/mods", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var body struct {
			ModIDs []int `json:"modIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		data := []map[string]any{}
		for _, id := range body.ModIDs {
			if m, ok := h.cfMods[id]; ok {
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
						data = append(data, fileJSON(f, m))
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
		if m.classID() == 6 && r.URL.Query().Get("modLoaderType") != "4" {
			writeJSON(w, map[string]any{"data": []any{}, "pagination": map[string]int{"totalCount": 0}})
			return
		}
		data := []map[string]any{}
		for _, f := range allFiles(m) {
			data = append(data, fileJSON(f, m))
		}
		writeJSON(w, map[string]any{"data": data, "pagination": map[string]int{"index": 0, "resultCount": len(data), "totalCount": len(data)}})
	})
	mux.HandleFunc("/cfcdn/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(strings.Split(strings.TrimPrefix(r.URL.Path, "/cfcdn/"), "/")[0])
		for _, m := range h.cfMods {
			for _, f := range allFiles(m) {
				if f.id == id {
					w.Write(f.jar.data)
					return
				}
			}
		}
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
	h.mustRun(t, "create", "--loader", "fabric")

	stdout := h.mustRun(t, "add", "jei")
	if !strings.Contains(stdout, "+ jei ") || strings.Contains(stdout, "»") || !strings.Contains(stdout, "+ fabric-api 1.0.0 (required by jei)") {
		t.Fatalf("add: %s", stdout)
	}
	l := h.readLock(t)
	jei := l.Mods["jei"]
	if jei.Provider != "curseforge" || jei.Project != "238222" || jei.Version != "5000001" || jei.Sha512 != h.jars["jei"].sha512 || jei.Size != int64(len(h.jars["jei"].data)) || jei.URL == nil || jei.Page != "" || jei.Side != "both" {
		t.Fatalf("jei lock entry: %+v", jei)
	}
	if dep := l.Mods["fabric-api"]; dep.Provider != "curseforge" || dep.RequiredBy[0] != "jei" {
		t.Fatalf("fabric-api lock entry: %+v", dep)
	}
	m := h.readManifest(t)
	if entry := m.Mods()["jei"]; entry.Project != "238222" || entry.Provider != "curseforge" {
		t.Fatalf("jei manifest entry: %+v", entry)
	}
	h.mustRun(t, "install")

	if stdout := h.mustRun(t, "outdated"); !strings.Contains(stdout, "Everything is up to date") {
		t.Fatalf("outdated before a new file: %s", stdout)
	}
	h.newer = true
	stdout = h.mustRun(t, "outdated")
	if !strings.Contains(stdout, "jei") || !strings.Contains(stdout, "1.1.0") {
		t.Fatalf("outdated: %s", stdout)
	}
	h.mustRun(t, "update")
	if v := h.readLock(t).Mods["jei"].Version; v != "5000002" {
		t.Fatalf("update left version %s", v)
	}
	h.mustRun(t, "pin", "jei", "5000001")
	if pin := h.readManifest(t).Mods()["jei"].Pin; pin != "5000001" {
		t.Fatalf("pin: %v", pin)
	}
	if v := h.readLock(t).Mods["jei"].Version; v != "5000001" {
		t.Fatalf("pin left version %s", v)
	}

	code, stdout, _ := h.run(t, "--json", "add", "nothing-anywhere")
	if e := failureCode(t, stdout); code == 0 || e.Code != "mod-not-found" || !strings.Contains(e.Message, "Modrinth or CurseForge") {
		t.Fatalf("expected mod-not-found, got %d %s", code, stdout)
	}

	stdout = h.mustRun(t, "add", "394468")
	if !strings.Contains(stdout, "+ sodium") || h.readLock(t).Mods["sodium"].Provider != "curseforge" {
		t.Fatalf("add by id: %s", stdout)
	}
}

func TestCurseForgeSwitchAndAbsenceOutput(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")

	_, stdout, _ := h.run(t, "--json", "add", "sodium", "--provider", "curseforge")
	var env struct {
		Data struct {
			Added   []map[string]any `json:"added"`
			Updated []map[string]any `json:"updated"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || len(env.Data.Added) != 0 || len(env.Data.Updated) != 1 || env.Data.Updated[0]["fromProvider"] != "modrinth" || env.Data.Updated[0]["toProvider"] != "curseforge" {
		t.Fatalf("add via curseforge: %s", stdout)
	}
	stdout = h.mustRun(t, "add", "sodium", "--provider", "modrinth")
	if !strings.HasPrefix(stdout, "  ~ sodium ") || !strings.Contains(stdout, "curseforge → modrinth") {
		t.Fatalf("switch back: %s", stdout)
	}
	if stdout = h.mustRun(t, "add", "sodium", "--provider", "curseforge"); !strings.Contains(stdout, "modrinth → curseforge") {
		t.Fatalf("second switch: %s", stdout)
	}
	if stdout = h.mustRun(t, "add", "sodium"); !strings.Contains(stdout, "sodium is already in the pack") {
		t.Fatalf("plain add after switch: %s", stdout)
	}

	h.noCurseForge = true
	code, stdout, _ := h.run(t, "--json", "add", "jei", "--provider", "curseforge")
	if e := failureCode(t, stdout); code == 0 || e.Code != "provider-unavailable" || !strings.Contains(e.Help, "SHULKER_CURSEFORGE_KEY") {
		t.Fatalf("expected provider-unavailable naming the key, got %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "--json", "add", "jei")
	if e := failureCode(t, stdout); code == 0 || e.Code != "mod-not-found" || !strings.Contains(e.Message, "not found on Modrinth") || len(e.Items) != 1 || !strings.HasPrefix(e.Items[0], "skipped: CurseForge needs an API key") {
		t.Fatalf("expected a modrinth miss naming the skipped curseforge, got %d %s", code, stdout)
	}
}
