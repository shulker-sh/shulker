package resolve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

type cfPackFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

// writeCurseForgeZip writes a CurseForge pack for fabric 26.2 listing files, with entries as its
// other contents and extras/ as its overrides folder.
func writeCurseForgeZip(t *testing.T, path string, files []cfPackFile, entries map[string]string) {
	t.Helper()
	m, err := json.Marshal(map[string]any{
		"minecraft":       map[string]any{"version": "26.2", "modLoaders": []map[string]any{{"id": "fabric-0.17.3", "primary": true}}},
		"manifestType":    "minecraftModpack",
		"manifestVersion": 1,
		"name":            "Craft Pack",
		"version":         "3.1",
		"author":          "someone",
		"files":           files,
		"overrides":       "extras",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries["manifest.json"] = string(m)
	if err := os.WriteFile(path, zipFiles(t, entries), 0o644); err != nil {
		t.Fatal(err)
	}
}

// curseForgeHost is a fake CurseForge: jei requiring fabric-api, sodium, a resource pack and two
// files that need a manual download, one undistributed and one forbidden.
func curseForgeHost(t *testing.T) *host {
	t.Helper()
	c := newCDN(t)
	cf := newHost(c, "curseforge").likeCurseForge()
	cf.Label = "CurseForge"
	cf.publish(mod("306612", "fabric-api"), provider.Version{ID: "5000010", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	cf.publish(mod("238222", "jei"), provider.Version{ID: "5000001", Number: "1.0.0", File: provider.File{Filename: "jei-26.2-fabric-1.0.0.jar"}, Dependencies: []provider.Dependency{dependsOn("306612")}}, modJar(t, "jei", "1.0.0", "*"))
	cf.publish(mod("394468", "sodium"), provider.Version{ID: "5000020", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}, Dependencies: []provider.Dependency{dependsOn("306612")}}, modJar(t, "sodium", "1.0.0", "client"))
	fresh := provider.Project{ID: "600000", Slug: "fresh-animations", Title: "Fresh Animations", Type: manifest.TypeResourcePack}
	cf.publish(fresh, provider.Version{ID: "5300001", Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_CF_v1.9.4.zip"}}, zipFiles(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"fresh"}}`}))
	cf.publishManual(mod("300000", "nodist"), provider.Version{ID: "5100001", Number: "1.0.0", File: provider.File{Filename: "nodist-1.0.0.jar"}}, modJar(t, "nodist", "1.0.0", "client"))
	c.forbid(cf.publish(mod("400000", "locked"), provider.Version{ID: "5200001", Number: "1.0.0", File: provider.File{Filename: "locked-1.0.0.jar"}}, modJar(t, "locked", "1.0.0", "*")))
	return cf
}

// importInto reads the archive and imports it into a fresh project in dir, the way the import
// command does for a new project, with an empty Modrinth beside cf for the overrides to be
// looked up on.
func importInto(t *testing.T, cf *host, dir, archive string) (*harness, *Imported, error) {
	t.Helper()
	arc, err := packarchive.Read(archive)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, newHost(cf.cdn, "modrinth"), cf)
	h.r.Dir = dir
	m, _ := arc.Manifest("craft-pack")
	h.r.Manifest = m
	rep, err := h.r.Import(context.Background(), arc)
	return h, rep, err
}

func TestImportLocksListedFilesAndSkipsOptionalOnes(t *testing.T) {
	cf := curseForgeHost(t)
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, []cfPackFile{
		{ProjectID: 238222, FileID: 5000001, Required: true},
		{ProjectID: 306612, FileID: 5000010, Required: true},
		{ProjectID: 600000, FileID: 5300001, Required: true},
		{ProjectID: 394468, FileID: 5000020, Required: false},
	}, map[string]string{
		"extras/config/jei.toml":  "jei = true\n",
		"extras/mods/bundled.jar": "not really a jar",
		"modlist.html":            "<ul></ul>",
	})

	h, res, err := importInto(t, cf, t.TempDir(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.LockedIDs(), ",") != "fabric-api,fresh-animations,jei" || strings.Join(res.Unmanaged, ",") != "overrides/mods/bundled.jar" || len(res.Reused) != 0 || len(res.Dropped) != 0 {
		t.Fatalf("mods: %+v", res)
	}
	var overrides []string
	for _, o := range res.Overrides {
		overrides = append(overrides, o.Layer+"/"+o.Path)
	}
	if strings.Join(overrides, ",") != "overrides/config/jei.toml,overrides/mods/bundled.jar" {
		t.Fatalf("overrides: %v", overrides)
	}
	if strings.Join(res.Warnings, "\n") != "skipped CurseForge project 394468 file 5000020: the pack marks it optional" {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	m, l := h.r.Manifest, h.r.Lock
	if m.Minecraft != "26.2" || m.Loader.Type != "fabric" || m.Loader.Version != "0.17.3" || strings.Join(m.Authors, ",") != "someone" || m.Version != "3.1" {
		t.Fatalf("manifest: %+v", m)
	}
	if jei := m.Mods()["jei"]; jei.Provider != "curseforge" || jei.Project != "238222" || jei.Pin != "" {
		t.Fatalf("jei manifest entry: %+v", jei)
	}
	if pack := m.ResourcePacks()["fresh-animations"]; pack.Provider != "curseforge" || pack.Project != "600000" || pack.Filename != "FreshAnimations_CF_v1.9.4.zip" {
		t.Fatalf("fresh-animations manifest entry: %+v", pack)
	}
	if api := m.Mods()["fabric-api"]; api.Provider != "curseforge" || api.Project != "306612" {
		t.Fatalf("a listed dependency is a manifest entry of its own: %+v", api)
	}
	jei := h.mod("jei")
	if jei.Provider != "curseforge" || jei.Version != "5000001" || jei.Sha512 != sha512Hex(h.cdn.bytes(cf.Files[1])) || jei.URL == nil {
		t.Fatalf("jei lock entry: %+v", jei)
	}
	if pack := l.ResourcePacks["fresh-animations"]; pack.Version != "5300001" || pack.Sha512 != sha512Hex(h.cdn.bytes(cf.Files[3])) || pack.Provider != "curseforge" || pack.Filename != "FreshAnimations_CF_v1.9.4.zip" {
		t.Fatalf("fresh-animations lock entry: %+v", pack)
	}
	if _, ok := l.Mods["sodium"]; ok {
		t.Fatal("an optional file was locked")
	}
}

func TestImportKeepsTheNewestOfTwoProjectsWithOneModID(t *testing.T) {
	older := cfPackFile{ProjectID: 282001, FileID: 6000001, Required: true}
	newer := cfPackFile{ProjectID: 1676502, FileID: 6000002, Required: true}
	for _, order := range [][]cfPackFile{{older, newer}, {newer, older}} {
		cf := curseForgeHost(t)
		cf.publish(mod("282001", "cc-tweaked"), provider.Version{ID: "6000001", Number: "1.113.1", File: provider.File{Filename: "cc-tweaked-1.113.1.jar"}}, modJar(t, "computercraft", "1.113.1", "*"))
		cf.publish(mod("1676502", "cc-tweaked-compat"), provider.Version{ID: "6000002", Number: "1.120.2", File: provider.File{Filename: "cc-tweaked-1.120.2.jar"}}, modJar(t, "computercraft", "1.120.2", "*"))
		archive := filepath.Join(t.TempDir(), "craft.zip")
		writeCurseForgeZip(t, archive, order, map[string]string{})

		h, res, err := importInto(t, cf, t.TempDir(), archive)
		if err != nil {
			t.Fatal(err)
		}
		if got := h.mod("computercraft"); got.Project != "1676502" || got.Filename != "cc-tweaked-1.120.2.jar" {
			t.Fatalf("lock entry: %+v", got)
		}
		if got := h.r.Manifest.Mods()["computercraft"]; got.Project != "1676502" {
			t.Fatalf("manifest entry: %+v", got)
		}
		if strings.Join(res.LockedIDs(), ",") != "computercraft" {
			t.Fatalf("locked: %v", res.LockedIDs())
		}
		first, second := "cc-tweaked-1.113.1.jar", "cc-tweaked-1.120.2.jar"
		if order[0] == newer {
			first, second = second, first
		}
		want := "computercraft appears twice in the pack (" + first + ", " + second + "); kept cc-tweaked-1.120.2.jar, the newest"
		if strings.Join(res.Warnings, "\n") != want {
			t.Fatalf("warnings: %q", res.Warnings)
		}
	}
}

func TestImportListsEveryManualDownload(t *testing.T) {
	cf := curseForgeHost(t)
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, []cfPackFile{
		{ProjectID: 238222, FileID: 5000001, Required: true},
		{ProjectID: 300000, FileID: 5100001, Required: true},
		{ProjectID: 400000, FileID: 5200001, Required: true},
	}, map[string]string{})
	dir := t.TempDir()
	nodist, locked := cf.Files[4], cf.Files[5]

	_, _, err := importInto(t, cf, dir, archive)
	e := out.AsError(err)
	if e == nil || e.Code != "missing-files" || len(e.Items) != 2 || !strings.Contains(e.Items[0], "nodist-1.0.0.jar from "+nodist.Page) || !strings.Contains(e.Items[1], "locked-1.0.0.jar") || !strings.Contains(e.Items[1], filepath.Join(dir, DownloadsDir)) {
		t.Fatalf("expected missing-files, got %v", err)
	}

	dropped := &harness{t: t, r: &Resolver{Dir: dir}}
	dropped.drop("nodist-1.0.0.jar", cf.cdn.bytes(nodist))
	dropped.drop("locked-1.0.0.jar", cf.cdn.bytes(locked))
	h, _, err := importInto(t, cf, dir, archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.mod("nodist"); got.URL != nil || got.Page != nodist.Page || got.Sha512 != sha512Hex(cf.cdn.bytes(nodist)) {
		t.Fatalf("nodist lock entry: %+v", got)
	}
	if got := h.mod("locked"); got.URL != nil || got.Page != locked.Page {
		t.Fatalf("locked lock entry: %+v", got)
	}
	if got := h.mod("jei"); got.URL == nil || got.Page != "" {
		t.Fatalf("jei lock entry: %+v", got)
	}
}

func TestImportNamesAListedFileItCouldNotDownload(t *testing.T) {
	cf := curseForgeHost(t)
	archive := filepath.Join(t.TempDir(), "craft.zip")
	writeCurseForgeZip(t, archive, []cfPackFile{{ProjectID: 394468, FileID: 5000020, Required: true}}, map[string]string{})
	cf.cdn.truncate(cf.Files[2])

	_, _, err := importInto(t, cf, t.TempDir(), archive)
	e := out.AsError(err)
	if e == nil || e.Code != "download-failed" || !strings.Contains(e.Message, "sodium (sodium-fabric-0.9.2+mc26.2.jar) from CurseForge") || e.Help == "" {
		t.Fatalf("expected download-failed naming the file, got %+v", e)
	}
}
