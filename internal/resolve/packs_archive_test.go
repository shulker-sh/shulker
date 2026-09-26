package resolve

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
)

var craftFiles = []cfPackFile{
	{ProjectID: 238222, FileID: 5000001, Required: true},
	{ProjectID: 306612, FileID: 5000010, Required: true},
	{ProjectID: 600000, FileID: 5300001, Required: true},
}

// craftArchive writes a CurseForge pack under the project at rel, with a config and a jar in its
// overrides folder and a modlist beside them, and reads it back as the modpack craft.
func craftArchive(t *testing.T, h *harness, rel string, files []cfPackFile, extras map[string]string) *modpack.Loaded {
	t.Helper()
	path := filepath.Join(h.r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{"modlist.html": "<ul></ul>"}
	for name, data := range extras {
		entries[name] = data
	}
	writeCurseForgeZip(t, path, files, entries)
	arc, err := packarchive.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(data)
	return &modpack.Loaded{Name: "craft", Source: rel, Kind: modpack.File, Archive: arc, Pin: lock.Modpack{File: rel, Sha512: hex.EncodeToString(sum[:]), Size: int64(len(data))}}
}

func craftExtras() map[string]string {
	return map[string]string{"extras/config/jei.toml": "jei = v1\n", "extras/mods/bundled.jar": "not really a jar"}
}

// consume locks the archive as the modpack's own, the way the pack store does before it adds the
// modpack to the project, and adds it to the project locked.
func (h *harness) consume(l *modpack.Loaded) error {
	h.t.Helper()
	if err := h.r.ConsumeArchive(context.Background(), l); err != nil {
		return err
	}
	l.UsesLock, l.Pin.UsesLock = true, true
	return h.r.AddPack(context.Background(), l)
}

func TestConsumeArchiveLocksAListedPackAsTheModpacksOwn(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, envtest.NewHost(cf.CDN, "modrinth"), cf)
	l := craftArchive(t, h, "packs/craft.zip", craftFiles, craftExtras())

	if err := h.consume(l); err != nil {
		t.Fatal(err)
	}
	if !l.UsesLock || l.Manifest == nil || l.Lock == nil || len(l.Manifest.Mods()) != 2 {
		t.Fatalf("the modpack's own manifest and lock: %+v", l)
	}
	mp := h.r.Lock.Modpacks["craft"]
	if mp.File != "packs/craft.zip" || mp.Sha512 == "" || mp.Size == 0 || !mp.UsesLock {
		t.Fatalf("modpack lock entry: %+v", mp)
	}
	if len(mp.Unmanaged) != 1 || mp.Unmanaged["overrides/mods/bundled.jar"] == "" {
		t.Fatalf("the bundled jar is the archive's own to lay: %v", mp.Unmanaged)
	}
	for _, id := range []string{"jei", "fabric-api"} {
		if m := h.mod(id); m.Modpack != "craft" || m.Provider != "curseforge" || !strings.Contains(strings.Join(m.RequiredBy, ","), "craft") && id == "jei" {
			t.Fatalf("%s is tagged to the pack: %+v", id, m)
		}
	}
	if p := h.r.Lock.ResourcePacks["fresh-animations"]; p.Modpack != "craft" || p.Sha512 != sha512Hex(cf.CDN.Bytes(cf.Files[3])) {
		t.Fatalf("fresh-animations is tagged to the pack: %+v", p)
	}
	if len(h.r.Manifest.Mods()) != 0 {
		t.Fatalf("a modpack's mods are not the project's own: %+v", h.r.Manifest.Requires)
	}
}

func TestConsumeArchiveListsAManualDownloadUntilItIsDropped(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, envtest.NewHost(cf.CDN, "modrinth"), cf)
	files := []cfPackFile{{ProjectID: 238222, FileID: 5000001, Required: true}, {ProjectID: 300000, FileID: 5100001, Required: true}}
	l := craftArchive(t, h, "packs/craft.zip", files, craftExtras())

	err := h.consume(l)
	if e := out.AsError(err); e == nil || e.Code != "missing-files" || len(e.Items) != 1 || !strings.Contains(e.Items[0], "nodist-1.0.0.jar") {
		t.Fatalf("expected missing-files, got %v", err)
	}

	h.drop("nodist-1.0.0.jar", cf.CDN.Bytes(cf.Files[4]))
	if err := h.consume(l); err != nil {
		t.Fatal(err)
	}
	nodist := h.mod("nodist")
	if nodist.URL != nil || !strings.Contains(nodist.Page, "nodist/files/5100001") || nodist.Modpack != "craft" || nodist.Sha512 != sha512Hex(cf.CDN.Bytes(cf.Files[4])) {
		t.Fatalf("nodist lock entry: %+v", nodist)
	}
}

func TestConsumeArchiveNeverResolvesOffline(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, envtest.NewHost(cf.CDN, "modrinth"), cf)
	l := craftArchive(t, h, "packs/craft.zip", craftFiles, craftExtras())
	h.r.Fetch.Offline = true

	err := h.r.ConsumeArchive(context.Background(), l)
	if e := out.AsError(err); e == nil || e.Code != "modpack-offline" || !strings.Contains(e.Message, "modpack craft") || cf.Requests["VersionsByID"] != 0 {
		t.Fatalf("an id-listed archive can't be read offline: %v %v", err, cf.Requests)
	}
}

func TestConsumeArchiveLeavesTheExportersMarkerOut(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, envtest.NewHost(cf.CDN, "modrinth"), cf)
	h.r.Lock.Java = lock.Java{Major: 21, Component: "java-runtime-delta"}
	h.r.Manifest.Schema, h.r.Manifest.Client = manifest.SchemaURL, &manifest.Client{}
	manifestData, err := h.r.Manifest.Encode()
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := h.r.Lock.Encode()
	if err != nil {
		t.Fatal(err)
	}
	marker := zipFiles(t, map[string]string{"fabric.mod.json": `{"id":"shulker-exporter","version":"1.0.0"}`, "shulker.json": string(manifestData), "shulker.lock": string(lockData)})
	l := craftArchive(t, h, "packs/craft.zip", craftFiles, map[string]string{"extras/mods/shulker-exporter.jar": string(marker)})

	if err := h.consume(l); err != nil {
		t.Fatal(err)
	}
	if u := h.r.Lock.Modpacks["craft"].Unmanaged; len(u) != 0 {
		t.Fatalf("the exporter's marker is not the pack's to lay: %v", u)
	}
}
