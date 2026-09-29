package cli

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

// archivePack writes a foreign .mrpack at rel in h's project: sodium for the client and fabric-api
// for both, which Modrinth knows by hash; extra for the server, which it doesn't; a jar dropped in
// the overrides; and a config file per layer. note varies the bytes.
func archivePack(t *testing.T, h *harness, rel, note string) fakeJar {
	t.Helper()
	extra := makeJar(t, "extra", "extra-1.0.jar", "*")
	file := func(jar fakeJar, side string) mrpackIndexFile {
		return mrpackIndexFile{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpackEnv(side), Downloads: []string{mrpackCDN + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpackIndex{
		FormatVersion: 1, Game: "minecraft", VersionID: "2.0", Name: "Someone's Pack",
		Files:        []mrpackIndexFile{file(h.jars["sodium"], "client"), file(h.jars["fabric-api"], "both"), file(extra, "server")},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	path := filepath.Join(h.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMrpack(t, path, index, map[string][]byte{
		"overrides/config/shared.txt":        []byte("shared " + note + "\n"),
		"client-overrides/config/client.txt": []byte("client\n"),
		"overrides/mods/local-1.0.jar":       []byte("not really a jar"),
	})
	h.jars["extra"] = extra
	return extra
}

func archiveProject(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"memory": "2G"}
	})
	return h
}

func TestArchiveModpackLocksAndBuilds(t *testing.T) {
	h := archiveProject(t)
	extra := archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	h.mustRun(t, "lock")

	l := h.readLock(t)
	mp := l.Modpacks["someone"]
	sha, err := fsutil.SHA512(filepath.Join(h.dir, "packs", "someone.mrpack"))
	if err != nil {
		t.Fatal(err)
	}
	if mp.File != "packs/someone.mrpack" || mp.Sha512 != sha || mp.Size == 0 || mp.Source != "" || !mp.UsesLock {
		t.Fatalf("modpack lock entry: %+v", mp)
	}
	if got := strings.Join(slices.Sorted(maps.Keys(mp.Unmanaged)), ","); got != "overrides/mods/local-1.0.jar,server-overrides/mods/extra-1.0.jar" {
		t.Fatalf("unmanaged: %v", mp.Unmanaged)
	}
	if mp.Unmanaged["server-overrides/mods/extra-1.0.jar"] != extra.sha512 {
		t.Fatalf("unmanaged digest: %v", mp.Unmanaged)
	}
	for _, id := range []string{"sodium", "fabric-api"} {
		if l.Mods[id].Modpack != "someone" {
			t.Fatalf("%s is tagged to the pack: %+v", id, l.Mods[id])
		}
	}
	if _, ok := l.Mods["extra"]; ok {
		t.Fatal("a file no provider knows is laid by the pack, not locked")
	}
	if !(&cache.Cache{Dir: h.cache}).Has(sha) {
		t.Fatal("the archive is put in the cache at lock time")
	}

	h.mustRun(t, "install")
	for _, rel := range []string{"client/mods/" + h.jars["sodium"].filename, "client/config/shared.txt", "client/config/client.txt", "client/mods/local-1.0.jar", "server/mods/extra-1.0.jar", "server/config/shared.txt"} {
		if _, err := os.Stat(filepath.Join(h.dir, "build", rel)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "server", "config", "client.txt")); !os.IsNotExist(err) {
		t.Fatalf("a client layer reaches the server: %v", err)
	}
}

func TestArchiveModpackSkipsAModTheProjectLists(t *testing.T) {
	h := archiveProject(t)
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{
			"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"},
			"sodium":  map[string]any{},
		}
	})
	h.mustRun(t, "lock")
	if l := h.readLock(t); l.Mods["sodium"].Modpack != "" {
		t.Fatalf("the project's own sodium wins: %+v", l.Mods["sodium"])
	}
	h.mustRun(t, "install")
	entries, err := os.ReadDir(filepath.Join(h.dir, "build", "client", "mods"))
	if err != nil {
		t.Fatal(err)
	}
	sodium := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sodium") {
			sodium++
		}
	}
	if sodium != 1 {
		t.Fatalf("one sodium jar, not the pack's as well: %v", entries)
	}
}

func TestArchiveModpackChangedBytes(t *testing.T) {
	h := archiveProject(t)
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	before := h.readLock(t).Modpacks["someone"].Sha512
	archivePack(t, h, "packs/someone.mrpack", "v2")

	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "someone: the file's bytes changed") {
		t.Fatalf("code=%d env=%+v", code, env)
	}

	h.mustRun(t, "lock")
	if after := h.readLock(t).Modpacks["someone"].Sha512; after == before {
		t.Fatal("lock re-reads the archive when its bytes change")
	}
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "config/shared.txt"); got != "shared v2\n" {
		t.Fatalf("the build lays the new archive's overrides: %q", got)
	}
}

func TestArchiveModpackWithoutAutoUpdateKeepsItsLock(t *testing.T) {
	h := archiveProject(t)
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack", "autoUpdate": false}}
	})
	h.mustRun(t, "lock")
	before := h.readLock(t).Modpacks["someone"].Sha512
	archivePack(t, h, "packs/someone.mrpack", "v2")

	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if h.readLock(t).Modpacks["someone"].Sha512 != before || env.LockStale {
		t.Fatalf("a pack that doesn't auto-update keeps the archive it locked: %+v", env)
	}
	h.mustRun(t, "update")
	if h.readLock(t).Modpacks["someone"].Sha512 == before {
		t.Fatal("update re-reads it")
	}
}

func TestArchiveModpackGoneBuildsFromCache(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	sha := h.readLock(t).Modpacks["someone"].Sha512
	if err := os.Remove(filepath.Join(h.dir, "packs", "someone.mrpack")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "cache", "prune")
	c := &cache.Cache{Dir: h.cache}
	if !c.Has(sha) {
		t.Fatal("cache prune keeps the archive")
	}
	if err := os.RemoveAll(filepath.Join(h.dir, "config")); err != nil {
		t.Fatal(err)
	}

	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "someone: packs/someone.mrpack is gone") {
		t.Fatalf("a gone archive warns once and builds: %+v", env)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "config", "shared.txt")); err != nil {
		t.Fatalf("the overrides come from the cached archive: %v", err)
	}
	env = out.Envelope{}
	if err := json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if h.readLock(t).Modpacks["someone"].Sha512 != sha {
		t.Fatalf("lock keeps the entry the cache serves: %+v", env)
	}
}

func TestAddModpackArchive(t *testing.T) {
	h := archiveProject(t)
	outside := filepath.Join(t.TempDir(), "proj")
	inside := h.dir
	h.dir = outside
	archivePack(t, h, "someone.mrpack", "v1")
	h.dir = inside

	h.mustRun(t, "modpack", "add", filepath.Join(outside, "someone.mrpack"))
	m := h.readManifest(t)
	entry, ok := m.Requires["someone"]
	if !ok || entry.File != "files/someone.mrpack" || entry.Type != "modpack" {
		t.Fatalf("an outside archive is copied into files/: %+v", m.Requires)
	}
	if l := h.readLock(t); l.Modpacks["someone"].File != "files/someone.mrpack" || l.Mods["sodium"].Modpack != "someone" {
		t.Fatalf("add locks it at once: %+v", l.Modpacks)
	}

	archivePack(t, h, "packs/other.mrpack", "v1")
	h.mustRun(t, "add", filepath.Join(h.dir, "packs", "other.mrpack"), "--as", "other")
	if entry := h.readManifest(t).Requires["other"]; entry.File != "packs/other.mrpack" || entry.Type != "modpack" {
		t.Fatalf("an archive inside the project is named where it lies, and a bare add reads it as a modpack: %+v", entry)
	}
}

func TestArchiveModpackRefusesANonPack(t *testing.T) {
	h := archiveProject(t)
	writeProjectFile(t, h, "packs/not.mrpack", makeJarFile(t, "faithful", "faithful.zip", "pack.mcmeta", `{"pack":{"pack_format":34}}`).data)
	code, stdout, _ := h.run(t, "modpack", "add", filepath.Join(h.dir, "packs", "not.mrpack"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "archive-not-modpack" {
		t.Fatalf("code=%d %s", code, stdout)
	}
}

func TestArchiveModpackSchema(t *testing.T) {
	h := archiveProject(t)
	for name, entry := range map[string]map[string]any{
		"side on a file modpack":   {"type": "modpack", "file": "packs/x.mrpack", "side": "client"},
		"ref on a file modpack":    {"type": "modpack", "file": "packs/x.mrpack", "ref": "main"},
		"locked with no origin":    {"type": "modpack", "locked": true},
		"autoUpdate on a provider": {"type": "modpack", "project": "x", "autoUpdate": false},
	} {
		h.editManifest(t, func(m map[string]any) {
			m["requires"] = map[string]any{"x": entry}
		})
		code, stdout, _ := h.run(t, "lock", "--json")
		if code == 0 || failureCode(t, stdout).Code != "manifest-invalid" {
			t.Errorf("%s: code=%d %s", name, code, stdout)
		}
	}
}

func TestArchiveModpackSyncsOfflineWhenUnchanged(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	h.mustRun(t, "sync", "--offline")
}

func TestUnlockedArchiveModpackSyncsOffline(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack", "locked": false}}
	})
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.mustRun(t, "lock")
	l := h.readLock(t)
	if l.Modpacks["someone"].UsesLock || l.Mods["sodium"].Modpack != "" || !slices.Contains(l.Mods["sodium"].RequiredBy, "someone") {
		t.Fatalf("an unlocked archive's mods are resolved here: %+v %+v", l.Modpacks["someone"], l.Mods["sodium"])
	}
	h.mustRun(t, "install")
	h.mustRun(t, "sync", "--offline")
	if after := h.readLock(t); after.Mods["sodium"].Sha512 != l.Mods["sodium"].Sha512 || !slices.Contains(after.Mods["sodium"].RequiredBy, "someone") {
		t.Fatalf("an offline relock keeps the archive's mods as they were: %+v", after.Mods["sodium"])
	}

	h.editManifest(t, func(m map[string]any) {
		delete(m["requires"].(map[string]any)["someone"].(map[string]any), "locked")
	})
	h.mustRun(t, "lock")
	if l := h.readLock(t); !l.Modpacks["someone"].UsesLock || l.Mods["sodium"].Modpack != "someone" {
		t.Fatalf("locking it reads the archive again: %+v %+v", l.Modpacks["someone"], l.Mods["sodium"])
	}
}

func TestChangedArchiveModpackOfflineIsACodedError(t *testing.T) {
	h := newInPlace(t)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"someone": map[string]any{"type": "modpack", "file": "packs/someone.mrpack"}}
	})
	archivePack(t, h, "packs/someone.mrpack", "v1")
	h.mustRun(t, "lock")
	h.mustRun(t, "install")
	archivePack(t, h, "packs/someone.mrpack", "v2")
	code, stdout, _ := h.run(t, "--json", "sync", "--offline")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-lookup" || !strings.Contains(e.Message, "modpack someone") {
		t.Fatalf("code=%d %+v", code, e)
	}
}

func TestArchiveModpackLaysAnExportedLocalFileItself(t *testing.T) {
	h := archiveProject(t)
	private := makeJar(t, "private-mod", "private-mod-1.4.jar", "*")
	if err := os.WriteFile(filepath.Join(h.dir, private.filename), private.data, 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "add", "./"+private.filename)
	h.mustRun(t, "export", "mrpack", "--version", "1", "--bundle", "-o", filepath.Join(h.dir, "packs", "mine.mrpack"))
	h.mustRun(t, "remove", "private-mod")
	h.mustRun(t, "add", "packs/mine.mrpack", "--as", "mine")

	l := h.readLock(t)
	if got, ok := l.Mods["private-mod"]; ok {
		t.Fatalf("the exporter's local file is not locked in the consumer: %+v", got)
	}
	if got := l.Modpacks["mine"].Unmanaged["overrides/mods/"+private.filename]; got != private.sha512 {
		t.Fatalf("the archive lays the jar as its own: %v", l.Modpacks["mine"].Unmanaged)
	}

	if err := os.Remove((&cache.Cache{Dir: h.cache}).Object(private.sha512)); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", private.filename)); err != nil {
		t.Fatalf("the jar comes from the cached archive: %v", err)
	}
}
