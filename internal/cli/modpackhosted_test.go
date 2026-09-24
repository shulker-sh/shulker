package cli

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
)

type modrinthPack struct {
	slug     string
	versions []modrinthPackVersion
}

type modrinthPackVersion struct {
	id, number, published string
	archive               fakeJar
	// loaders are the loaders the version is tagged with, fabric when empty.
	loaders []string
}

// hostedMrpack builds a Modrinth pack archive served from the fake CDN: sodium and fabric-api, which
// Modrinth knows by hash, and a config file whose contents note varies.
func hostedMrpack(t *testing.T, h *harness, filename, note string) fakeJar {
	t.Helper()
	file := func(jar fakeJar, side string) mrpackIndexFile {
		return mrpackIndexFile{Path: "mods/" + jar.filename, Hashes: map[string]string{"sha1": jar.sha1, "sha512": jar.sha512}, Env: mrpackEnv(side), Downloads: []string{h.server.URL + "/cdn/" + jar.filename}, FileSize: int64(len(jar.data))}
	}
	index := mrpackIndex{
		FormatVersion: 1, Game: "minecraft", VersionID: note, Name: "Cozy",
		Files:        []mrpackIndexFile{file(h.jars["sodium"], "client"), file(h.jars["fabric-api"], "both")},
		Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.17.3"},
	}
	path := filepath.Join(t.TempDir(), filename)
	writeMrpack(t, path, index, map[string][]byte{"overrides/config/cozy.txt": []byte("cozy " + note + "\n")})
	return archiveJar(t, h, filename, path)
}

func archiveJar(t *testing.T, h *harness, filename, path string) fakeJar {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum, sum1 := sha512.Sum512(data), sha1.Sum(data)
	jar := fakeJar{id: filename, filename: filename, data: data, sha512: hex.EncodeToString(sum[:]), sha1: hex.EncodeToString(sum1[:])}
	h.jars[filename] = jar
	return jar
}

func hostedProject(t *testing.T) (*harness, fakeJar) {
	t.Helper()
	h := archiveProject(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: archive}}}}
	return h, archive
}

func TestPinningAnUnknownModpackVersionLinksItsVersions(t *testing.T) {
	h, _ := hostedProject(t)
	code, stdout, _ := h.run(t, "add", "cozy", "--pin", "nope", "--json")
	if code != 1 || !strings.Contains(stdout, `"version-not-found"`) || !strings.Contains(stdout, "modrinth.com/modpack/cozy/versions") {
		t.Fatalf("add a modpack pinned to an unknown version: %d %s", code, stdout)
	}
}

func TestHostedModpackAddLocksAndBuilds(t *testing.T) {
	h, archive := hostedProject(t)
	h.mustRun(t, "add", "cozy")

	entry := h.readManifest(t).Requires["cozy"]
	if entry.Type != "modpack" || entry.Provider != "modrinth" || entry.Project != "COZYpack" || entry.Source != "" || entry.File != "" {
		t.Fatalf("manifest entry: %+v", entry)
	}
	l := h.readLock(t)
	mp := l.Modpacks["cozy"]
	if mp.Provider != "modrinth" || mp.Project != "COZYpack" || mp.Version != "cozyV100" || mp.VersionNumber != "1.0.0" || mp.Channel != "release" ||
		mp.URL == nil || mp.Filename != "cozy-1.0.0.mrpack" || mp.Sha512 != archive.sha512 || mp.Size != int64(len(archive.data)) || !mp.UsesLock || mp.Source != "" {
		t.Fatalf("modpack lock entry: %+v", mp)
	}
	for _, id := range []string{"sodium", "fabric-api"} {
		if l.Mods[id].Modpack != "cozy" {
			t.Fatalf("%s is tagged to the pack: %+v", id, l.Mods[id])
		}
	}
	h.mustRun(t, "install")
	for _, rel := range []string{"client/mods/" + h.jars["sodium"].filename, "client/config/cozy.txt", "server/config/cozy.txt"} {
		if _, err := os.Stat(filepath.Join(h.dir, "build", rel)); err != nil {
			t.Fatal(err)
		}
	}
	if list := h.mustRun(t, "list"); !strings.Contains(list, "cozy 1.0.0 (modpack)") {
		t.Fatalf("list shows the modpack's version:\n%s", list)
	}

	h.mustRun(t, "cache", "prune")
	if !(&cache.Cache{Dir: h.cache}).Has(archive.sha512) {
		t.Fatal("cache prune keeps a hosted modpack's archive")
	}
}

func TestHostedModpackSyncsOffline(t *testing.T) {
	h := newInPlace(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: archive}}}}
	h.mustRun(t, "add", "cozy")
	h.mustRun(t, "install")
	next := hostedMrpack(t, h, "cozy-2.0.0.mrpack", "2.0.0")
	h.modrinthPacks["COZYpack"].versions = append(h.modrinthPacks["COZYpack"].versions, modrinthPackVersion{id: "cozyV200", number: "2.0.0", published: "2026-09-05T00:00:00Z", archive: next})
	h.mustRun(t, "sync", "--offline")
	if after := h.readLock(t).Modpacks["cozy"]; after.Version != "cozyV100" {
		t.Fatalf("an offline sync keeps the locked version: %+v", after)
	}
	h.mustRun(t, "sync")
	if after := h.readLock(t).Modpacks["cozy"]; after.Version != "cozyV100" {
		t.Fatalf("sync never moves a hosted modpack: %+v", after)
	}
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["cozy"].(map[string]any)["pin"] = "cozyV200"
	})
	code, _, stderr := h.run(t, "sync", "--offline")
	if code == 0 || !strings.Contains(stderr, "offline") {
		t.Fatalf("a version the lock doesn't hold can't be fetched offline: %d\n%s", code, stderr)
	}
}

func TestHostedModpackInheritsItsPlatform(t *testing.T) {
	h, _ := hostedProject(t)
	h.editManifest(t, func(m map[string]any) {
		delete(m, "minecraft")
		delete(m, "loader")
	})
	h.mustRun(t, "add", "cozy")
	l := h.readLock(t)
	if l.Minecraft != "26.2" || l.Loader.Type != "fabric" || l.Loader.Version != "0.17.3" {
		t.Fatalf("the project inherits the pack's platform: %s %+v", l.Minecraft, l.Loader)
	}
}

func TestHostedModpackUpdatesLikeAMod(t *testing.T) {
	h, _ := hostedProject(t)
	h.mustRun(t, "add", "cozy")
	next := hostedMrpack(t, h, "cozy-2.0.0.mrpack", "2.0.0")
	cozy := h.modrinthPacks["COZYpack"]
	cozy.versions = append(cozy.versions, modrinthPackVersion{id: "cozyV200", number: "2.0.0", published: "2026-09-05T00:00:00Z", archive: next})

	if got := h.mustRun(t, "outdated"); !strings.Contains(got, "cozy 1.0.0 ⟶ 2.0.0 (modpack)") {
		t.Fatalf("outdated reports the modpack:\n%s", got)
	}
	h.mustRun(t, "pin", "cozy")
	if pin := h.readManifest(t).Requires["cozy"].Pin; pin != "cozyV100" {
		t.Fatalf("pin holds the locked version: %v", pin)
	}
	h.mustRun(t, "update")
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV100" {
		t.Fatalf("update leaves a pinned modpack: %v", v)
	}
	if got := h.mustRun(t, "outdated", "cozy"); !strings.Contains(got, "(modpack, pinned)") {
		t.Fatalf("outdated marks the pinned modpack:\n%s", got)
	}
	h.mustRun(t, "unpin", "cozy")
	l := h.readLock(t)
	if mp := l.Modpacks["cozy"]; mp.Version != "cozyV200" || mp.Sha512 != next.sha512 {
		t.Fatalf("unpin moves the modpack to its newest version: %+v", mp)
	}
	h.mustRun(t, "install")
	data, err := os.ReadFile(filepath.Join(h.dir, "build", "client", "config", "cozy.txt"))
	if err != nil || string(data) != "cozy 2.0.0\n" {
		t.Fatalf("the build lays the new version's overrides: %q %v", data, err)
	}
	h.mustRun(t, "pin", "cozy", "cozyV100")
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV100" {
		t.Fatalf("pin to a version locks it: %v", v)
	}
	h.mustRun(t, "unpin", "cozy")
	h.mustRun(t, "lock")
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV200" {
		t.Fatalf("unpin locks the newest version: %v", v)
	}
	third := hostedMrpack(t, h, "cozy-3.0.0.mrpack", "3.0.0")
	cozy.versions = append(cozy.versions, modrinthPackVersion{id: "cozyV300", number: "3.0.0", published: "2026-09-09T00:00:00Z", archive: third})
	h.mustRun(t, "lock")
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV200" {
		t.Fatalf("lock keeps the locked version: %v", v)
	}
	if got := h.mustRun(t, "update"); !strings.Contains(got, "3.0.0") {
		t.Fatalf("update reports the modpack moving:\n%s", got)
	}
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV300" {
		t.Fatalf("update moves an unpinned modpack: %v", v)
	}
}

func TestHostedModpackRefusesSourceFlags(t *testing.T) {
	h, _ := hostedProject(t)
	for _, tc := range []struct {
		args   []string
		reason string
	}{
		{[]string{"add", "--type", "modpack", "cozy", "--ref", "main"}, "a ref names a git commit"},
		{[]string{"add", "--type", "modpack", "cozy", "--unlocked"}, "always locked"},
		{[]string{"add", "--type", "modpack", "cozy", "--no-auto-update"}, "never auto-updates"},
		{[]string{"add", "cozy", "--side", "client"}, "its mods carry their own sides"},
		{[]string{"add", "--type", "modpack", "../elsewhere", "--pin", "cozyV100"}, "only applies to a modpack from a provider"},
	} {
		code, _, stderr := h.run(t, tc.args...)
		if code == 0 || !strings.Contains(stderr, tc.reason) {
			t.Errorf("%v: want a refusal naming %q, got %d:\n%s", tc.args, tc.reason, code, stderr)
		}
	}
	if _, ok := h.readManifest(t).Requires["cozy"]; ok {
		t.Fatal("a refused add writes nothing")
	}
	h.mustRun(t, "modpack", "add", "cozy", "--channel", "beta", "--as", "snug")
	if entry := h.readManifest(t).Requires["snug"]; entry.Channel != "beta" || entry.Project != "COZYpack" {
		t.Fatalf("modpack add takes a slug with --channel and --as: %+v", entry)
	}
}

func TestHostedModpackNoCompatibleVersion(t *testing.T) {
	h, _ := hostedProject(t)
	h.modrinthPacks["COZYpack"].versions = nil
	code, _, stderr := h.run(t, "add", "cozy")
	if code == 0 || !strings.Contains(stderr, "no-compatible-version") {
		t.Fatalf("a pack with no version for the platform is refused: %d\n%s", code, stderr)
	}
}

func TestHandWrittenHostedModpackLocks(t *testing.T) {
	h, archive := hostedProject(t)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"cozy": map[string]any{"type": "modpack"}}
	})
	h.mustRun(t, "lock")
	if mp := h.readLock(t).Modpacks["cozy"]; mp.Provider != "modrinth" || mp.Sha512 != archive.sha512 {
		t.Fatalf("project defaults to the key and provider to the first in order: %+v", mp)
	}
	h.mustRun(t, "lock")
	if stdout := h.mustRun(t, "list", "--type", "modpack"); !strings.Contains(stdout, "cozy 1.0.0") {
		t.Fatalf("list:\n%s", stdout)
	}
}
