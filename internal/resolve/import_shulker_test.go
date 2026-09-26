package resolve

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

// shulkerExport is a CurseForge pack a shulker project exported with its marker: sodium behind a
// feature and fabric-api from Modrinth, both listed by the CurseForge ids their fingerprints
// matched, jei from CurseForge under the key recipes, and a private jar bundled as an override.
// modrinth and cf share one cdn.
func shulkerExport(t *testing.T, dir string) (archive string, modrinth, cf *envtest.Host) {
	t.Helper()
	c := envtest.NewCDN(t)
	modrinth, cf = envtest.NewHost(c, "modrinth"), envtest.NewHost(c, "curseforge").LikeCurseForge()
	sodiumJar, apiJar, jeiJar := modJar(t, "sodium", "1.0.0", "client"), modJar(t, "fabric-api", "1.0.0", "*"), modJar(t, "jei", "1.0.0", "*")
	sodium := modrinth.Publish(mod("AANobbMI", "sodium"), provider.Version{ID: "QANobbMI", Number: "1.0.0", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, sodiumJar)
	api := modrinth.Publish(mod("P7dR8mSH", "fabric-api"), provider.Version{ID: "QP7dR8mSH", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, apiJar)
	cf.Publish(mod("394468", "sodium"), provider.Version{ID: "5000020", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, sodiumJar)
	cf.Publish(mod("306612", "fabric-api"), provider.Version{ID: "5000010", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, apiJar)
	jei := cf.Publish(mod("238222", "jei"), provider.Version{ID: "5000001", Number: "1.0.0", File: provider.File{Filename: "jei-26.2-fabric-1.0.0.jar"}}, jeiJar)
	private := modJar(t, "private-mod", "1.4", "client")

	m := &manifest.Manifest{
		Schema: manifest.SchemaURL, Name: "pack", Version: "1.0", Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric"},
		Providers: []string{"modrinth", "curseforge"},
		Features:  map[string]manifest.Feature{"fast": {Default: true}},
		Requires: map[string]manifest.Require{
			"sodium":      {Provider: "modrinth", Feature: manifest.StringList{"fast"}},
			"fabric-api":  {Provider: "modrinth"},
			"recipes":     {Provider: "curseforge", Project: "238222"},
			"private-mod": {File: "files/private-mod-1.4.jar"},
		},
		Client: &manifest.Client{}, Server: &manifest.Server{},
	}
	locked := func(h *envtest.Host, v provider.Version, side string) lock.Mod {
		return lock.Mod{Provider: h.Name(), Project: v.ProjectID, Version: v.ID, VersionNumber: v.Number, Filename: v.File.Filename, URL: &v.File.URL, Sha512: sha512Hex(c.Bytes(v)), Size: v.File.Size, Side: side, Channel: "release", RequiredBy: []string{}, Aliases: lock.Aliases{}}
	}
	l := lock.New()
	l.Minecraft, l.Loader, l.Java = "26.2", lock.Loader{Type: "fabric", Version: "0.17.3"}, lock.Java{Major: 21, Component: "java-runtime-delta"}
	l.Mods["sodium"] = locked(modrinth, sodium, "client")
	l.Mods["fabric-api"] = locked(modrinth, api, "both")
	l.Mods["recipes"] = locked(cf, jei, "both")
	l.Mods["private-mod"] = lock.Mod{File: "files/private-mod-1.4.jar", Filename: "private-mod-1.4.jar", Sha512: sha512Hex(private), Size: int64(len(private)), Side: "client", RequiredBy: []string{}, Aliases: lock.Aliases{}}
	mData, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	lData, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	format, _ := packarchive.Lookup("curseforge")
	archive = filepath.Join(dir, "pack-1.0.zip")
	err = packarchive.Write(format, archive, &packarchive.Export{
		Name: "pack", Version: "1.0", Authors: []string{"Ann", "Bo"}, Minecraft: "26.2", Loader: packarchive.Loader{Type: "fabric", Version: "0.17.3"},
		Files: []packarchive.File{
			{Provider: "curseforge", Project: "306612", Version: "5000010"},
			{Provider: "curseforge", Project: "238222", Version: "5000001"},
			{Provider: "curseforge", Project: "394468", Version: "5000020"},
		},
		Overrides: []packarchive.Override{{Layer: "overrides", Path: "mods/private-mod-1.4.jar", Data: private}},
		Manifest:  mData, Lock: lData,
	})
	if err != nil {
		t.Fatal(err)
	}
	return archive, modrinth, cf
}

// importArchive reads archive and imports it into a fresh project in dir on the providers given,
// with the archive's own manifest as the project's.
func importArchive(t *testing.T, dir, archive string, ignoreMarker bool, providers ...*envtest.Host) (*harness, *Imported, error) {
	t.Helper()
	arc, err := packarchive.Read(archive)
	if err != nil {
		t.Fatal(err)
	}
	if ignoreMarker {
		arc.Marker = nil
	}
	h := newHarness(t, providers...)
	h.r.Dir = dir
	m, _ := arc.Manifest("pack")
	h.r.Manifest = m
	rep, err := h.r.Import(context.Background(), arc)
	return h, rep, err
}

func TestImportRestoresAShulkerExport(t *testing.T) {
	archive, modrinth, cf := shulkerExport(t, t.TempDir())
	dir := t.TempDir()

	h, res, err := importArchive(t, dir, archive, false, modrinth, cf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Reused, ",") != "fabric-api,private-mod,recipes,sodium" || len(res.Locked) != 0 || len(res.Dropped) != 0 || len(res.Unmanaged) != 0 {
		t.Fatalf("result: %+v", res)
	}
	m, l := h.r.Manifest, h.r.Lock
	if _, ok := m.Features["fast"]; !ok || m.Server == nil || m.Version != "1.0" {
		t.Fatalf("manifest: %+v", m)
	}
	if sodium := m.Requires["sodium"]; strings.Join(sodium.Feature, ",") != "fast" || l.Mods["sodium"].Provider != "modrinth" || l.Mods["sodium"].Version != "QANobbMI" {
		t.Fatalf("sodium comes back from Modrinth behind its feature: %+v %+v", sodium, l.Mods["sodium"])
	}
	if l.Mods["recipes"].Provider != "curseforge" || l.Mods["recipes"].Version != "5000001" || l.Mods["jei"].Sha512 != "" {
		t.Fatalf("jei keeps its key: %+v", l.Mods["recipes"])
	}
	if m.Requires["private-mod"].File != "files/private-mod-1.4.jar" || l.Mods["private-mod"].File != "files/private-mod-1.4.jar" {
		t.Fatalf("the bundled jar is the project's own: %+v", l.Mods["private-mod"])
	}
	if err := h.r.AdoptLocalFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "private-mod-1.4.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "overrides", "mods", "private-mod-1.4.jar")); err == nil {
		t.Fatal("the bundled jar is also an override")
	}
}

func TestImportWithoutTheMarkerLocksWhatThePackLists(t *testing.T) {
	archive, modrinth, cf := shulkerExport(t, t.TempDir())

	h, res, err := importArchive(t, t.TempDir(), archive, true, modrinth, cf)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reused) != 0 || strings.Join(res.LockedIDs(), ",") != "fabric-api,jei,sodium" || strings.Join(res.Unmanaged, ",") != "overrides/mods/private-mod-1.4.jar" {
		t.Fatalf("result: %+v", res)
	}
	if m, l := h.r.Manifest, h.r.Lock; len(m.Features) != 0 || m.Server != nil || l.Mods["sodium"].Provider != "curseforge" || l.Mods["sodium"].Version != "5000020" {
		t.Fatalf("an ignored project leaves nothing behind: %+v %+v", m, l.Mods["sodium"])
	}
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// writeMrpack writes a Modrinth pack for fabric 26.2 listing files, with entries as its other
// contents.
func writeMrpack(t *testing.T, path string, files []mrpackFile, entries map[string]string) {
	t.Helper()
	index, err := json.Marshal(map[string]any{
		"formatVersion": 1, "game": "minecraft", "versionId": "1.0", "name": "Mixed",
		"files":        files,
		"dependencies": map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries["modrinth.index.json"] = string(index)
	if err := os.WriteFile(path, zipFiles(t, entries), 0o644); err != nil {
		t.Fatal(err)
	}
}

// listedByDownload lists v the way an mrpack does, by hashes and a download of its own, for both
// sides.
func listedByDownload(c *envtest.CDN, v provider.Version) mrpackFile {
	data := c.Bytes(v)
	url := c.Serve("/mrpack/"+v.File.Filename, data)
	return mrpackFile{Path: "mods/" + v.File.Filename, Hashes: map[string]string{"sha1": v.File.Sha1, "sha512": sha512Hex(data)}, Env: map[string]string{"client": "required", "server": "required"}, Downloads: []string{url}, FileSize: int64(len(data))}
}

func TestImportIdentifiesTheFilesItCannotLockByHashOnCurseForge(t *testing.T) {
	cf := curseForgeHost(t)
	modrinth := envtest.NewHost(cf.CDN, "modrinth")
	sodium := modrinth.Publish(mod("AANobbMI", "sodium"), provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2", "client"))
	jei, nodist := cf.Files[1], cf.Files[4]
	iris := cf.Publish(mod("455508", "irisshaders"), provider.Version{ID: "5500001", Number: "1.11.3", File: provider.File{Filename: "iris-fabric-1.11.3+mc26.2.jar"}}, modJar(t, "iris", "1.11.3", "client"))
	archive := filepath.Join(t.TempDir(), "mixed.mrpack")
	writeMrpack(t, archive, []mrpackFile{listedByDownload(cf.CDN, jei), listedByDownload(cf.CDN, nodist), listedByDownload(cf.CDN, sodium)}, map[string]string{
		"client-overrides/mods/" + iris.File.Filename: string(cf.CDN.Bytes(iris)),
		"overrides/mods/unknown-1.0.jar":              "not on any provider",
	})
	importMixed := func(t *testing.T) (*harness, *Imported) {
		t.Helper()
		h, res, err := importArchive(t, t.TempDir(), archive, false, modrinth, cf)
		if err != nil {
			t.Fatal(err)
		}
		return h, res
	}

	cf.CDN.Truncate(iris)
	cf.Requests, modrinth.Requests = map[string]int{}, map[string]int{}
	h, res := importMixed(t)
	if modrinth.Requests["Identify"] != 1 || cf.Requests["Identify"] != 1 {
		t.Fatalf("each provider identifies the unlocked files in one batch: %v %v", modrinth.Requests, cf.Requests)
	}
	if strings.Join(res.LockedIDs(), ",") != "jei,sodium" || strings.Join(res.Unmanaged, ",") != "client-overrides/mods/"+iris.File.Filename+",overrides/mods/"+nodist.File.Filename+",overrides/mods/unknown-1.0.jar" {
		t.Fatalf("import: %+v", res)
	}
	if len(res.Warnings) != 2 || !strings.Contains(res.Warnings[0], nodist.File.Filename) || !strings.Contains(res.Warnings[0], "third-party downloads") ||
		!strings.Contains(res.Warnings[1], iris.File.Filename) || !strings.Contains(res.Warnings[1], "download failed") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	if _, ok := h.r.Lock.Mods["iris"]; ok || h.mod("jei").Provider != "curseforge" || h.mod("sodium").Provider != "modrinth" {
		t.Fatalf("lock: %+v", h.r.Lock.Mods)
	}
	if got := h.r.Manifest.Requires["jei"]; got.Provider != "curseforge" || got.Project != "238222" {
		t.Fatalf("jei entry: %+v", got)
	}
	if !slices.ContainsFunc(res.Overrides, func(o packarchive.Override) bool {
		return o.Layer == "client-overrides" && o.Path == "mods/"+iris.File.Filename
	}) {
		t.Fatalf("a jar whose download failed is kept as an override: %+v", res.Overrides)
	}

	cf.CDN.Restore(iris)
	h, res = importMixed(t)
	if strings.Join(res.LockedIDs(), ",") != "iris,jei,sodium" || h.mod("iris").Side != "client" {
		t.Fatalf("import once CurseForge serves iris: %+v", res)
	}
	if slices.ContainsFunc(res.Overrides, func(o packarchive.Override) bool { return o.Path == "mods/"+iris.File.Filename }) {
		t.Fatalf("a locked jar is kept as an override: %+v", res.Overrides)
	}

	cf.Unavailable = errors.New("curseforge needs an API key")
	_, res = importMixed(t)
	if strings.Join(res.LockedIDs(), ",") != "sodium" || len(res.Unmanaged) != 4 {
		t.Fatalf("import without CurseForge: %+v", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "CurseForge") {
		t.Fatalf("warnings without CurseForge: %v", res.Warnings)
	}
}

func TestImportTakesAModsSideFromThePackOnlyWhereItAddsABuiltSide(t *testing.T) {
	cf := curseForgeHost(t)
	modrinth := envtest.NewHost(cf.CDN, "modrinth")
	sodium := modrinth.Publish(mod("AANobbMI", "sodium"), provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2", "client"))
	configManager := cf.Publish(mod("800000", "config-manager"), provider.Version{ID: "5600001", Number: "1.0.0", File: provider.File{Filename: "config_manager-1.0.0.jar"}}, modJar(t, "config_manager", "1.0.0", "server"))
	serverTweaks := cf.Publish(mod("800001", "server-tweaks"), provider.Version{ID: "5600002", Number: "1.0.0", File: provider.File{Filename: "server_tweaks-1.0.0.jar"}}, modJar(t, "server_tweaks", "1.0.0", "server"))
	jei, api := cf.Files[1], cf.Files[0]
	listed := func(v provider.Version, env map[string]string) mrpackFile {
		f := listedByDownload(cf.CDN, v)
		f.Env = env
		return f
	}
	sided := func(side string) map[string]string {
		env := map[string]string{"client": "required", "server": "required"}
		switch side {
		case "client":
			env["server"] = "unsupported"
		case "server":
			env["client"] = "unsupported"
		}
		return env
	}
	archive := filepath.Join(t.TempDir(), "singleplayer.mrpack")
	writeMrpack(t, archive, []mrpackFile{
		listed(configManager, sided("both")),
		listed(serverTweaks, sided("client")),
		listed(sodium, sided("both")),
		listed(jei, sided("client")),
		listed(api, nil),
	}, map[string]string{})

	h, res, err := importArchive(t, t.TempDir(), archive, false, modrinth, cf)
	if err != nil {
		t.Fatal(err)
	}
	if want := []SideChoice{{ID: "config_manager", Pack: "both", Provider: "server"}, {ID: "server_tweaks", Pack: "both", Provider: "server"}}; !slices.Equal(res.Sides, want) {
		t.Fatalf("sides: %+v", res.Sides)
	}
	sideWarnings := slices.DeleteFunc(slices.Clone(res.Warnings), func(w string) bool { return !strings.Contains(w, "take their side") })
	if len(sideWarnings) != 1 || !strings.Contains(sideWarnings[0], "2 mods") || !strings.Contains(sideWarnings[0], "config_manager (server → both), server_tweaks (server → both)") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	m, l := h.r.Manifest, h.r.Lock
	for _, id := range []string{"config_manager", "server_tweaks"} {
		if l.Mods[id].Side != "both" || m.Requires[id].Side != "both" {
			t.Errorf("%s: lock %+v, manifest %+v", id, l.Mods[id], m.Requires[id])
		}
	}
	for id, side := range map[string]string{"sodium": "client", "jei": "both", "fabric-api": "both"} {
		if l.Mods[id].Side != side || m.Requires[id].Side != "" {
			t.Errorf("%s: lock %+v, manifest %+v", id, l.Mods[id], m.Requires[id])
		}
	}
}
