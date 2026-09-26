package resolve

import (
	"context"
	"maps"
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

func TestAlreadyRequiredKeysLegacyIDBySlug(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "overrides", "mods", "SpawnerControl.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, zipBytes(t, "mcmod.info", `[{"modid": "SpawnerControl", "version": "1.6.3b"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{
		Dir:      dir,
		Manifest: &manifest.Manifest{Requires: map[string]manifest.Require{"mob-spawner-control": {}}},
		Lock:     &lock.Lock{Minecraft: "1.12.2", Loader: lock.Loader{Type: "forge", Version: "14.23.5.2860"}, Mods: map[string]lock.Mod{}},
	}
	im := &importer{r: r, rep: &Imported{}, inProject: true}
	o := packarchive.Override{Layer: "overrides", Path: "mods/SpawnerControl.jar"}
	if !im.alreadyRequired(o, &provider.Project{Slug: "mob-spawner-control"}) {
		t.Fatalf("a legacy mod required under its slug isn't found: %v", im.rep.Warnings)
	}
}

// overrideFiles writes each file under the project and lists it as an override to match.
func overrideFiles(t *testing.T, dir string, files map[string][]byte) []packarchive.Override {
	t.Helper()
	var overrides []packarchive.Override
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, files[rel], 0o644); err != nil {
			t.Fatal(err)
		}
		layer, p, _ := strings.Cut(rel, "/")
		overrides = append(overrides, packarchive.Override{Layer: layer, Path: p, Data: files[rel]})
	}
	return overrides
}

func TestMatchOverridesLocksTheFilesProvidersHost(t *testing.T) {
	cf := curseForgeHost(t)
	iris := cf.Publish(mod("455508", "irisshaders"), provider.Version{ID: "5000030", Number: "1.8.0", File: provider.File{Filename: "iris-fabric-1.8.0+mc26.2.jar"}}, modJar(t, "iris", "1.8.0", "client"))
	modrinth := envtest.NewHost(cf.CDN, "modrinth")
	sodium := modrinth.Publish(mod("AANobbMI", "sodium"), provider.Version{Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "1.0.0", "client"))
	fresh := modrinth.Publish(provider.Project{ID: "fresh", Slug: "fresh-animations", Type: manifest.TypeResourcePack}, provider.Version{Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "fresh-animations-1.9.4.zip"}}, zipFiles(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34,"description":"fresh"}}`}))
	h := newHarness(t, modrinth, cf)
	jei, nodist := cf.Files[1], cf.Files[4]
	files := overrideFiles(t, h.r.Dir, map[string][]byte{
		"overrides/mods/" + sodium.File.Filename:              cf.CDN.Bytes(sodium),
		"overrides/mods/" + jei.File.Filename:                 cf.CDN.Bytes(jei),
		"client-overrides/mods/" + iris.File.Filename:         cf.CDN.Bytes(iris),
		"overrides/mods/" + nodist.File.Filename:              cf.CDN.Bytes(nodist),
		"overrides/mods/unknown-1.0.jar":                      []byte("not on any provider"),
		"client-overrides/resourcepacks/Fresh Animations.zip": cf.CDN.Bytes(fresh),
	})

	res, err := h.r.MatchOverrides(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	wantLocked := []LockedFile{
		{ID: "fresh-animations", Type: "resourcepack", Provider: "modrinth"},
		{ID: "iris", Type: "mod", Provider: "curseforge"},
		{ID: "jei", Type: "mod", Provider: "curseforge"},
		{ID: "sodium", Type: "mod", Provider: "modrinth"},
	}
	if !slices.Equal(res.Locked, wantLocked) || strings.Join(res.Kept, ",") != "overrides/mods/"+nodist.File.Filename+",overrides/mods/unknown-1.0.jar" || len(res.Moved) != 4 {
		t.Fatalf("match: %+v", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], nodist.File.Filename) || !strings.Contains(res.Warnings[0], "third-party downloads") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	if h.mod("sodium").Provider != "modrinth" || h.mod("jei").Provider != "curseforge" || h.mod("iris").Side != "client" || h.r.Lock.ResourcePacks["fresh-animations"].Filename != "Fresh Animations.zip" {
		t.Fatalf("lock: %+v %+v", h.r.Lock.Mods, h.r.Lock.ResourcePacks)
	}
	if _, ok := h.r.Manifest.Requires["jei"]; !ok {
		t.Fatalf("requires: %+v", h.r.Manifest.Requires)
	}
	if modrinth.Requests["Identify"] != 1 || cf.Requests["Identify"] != 1 {
		t.Fatalf("one round of lookups per provider: %v %v", modrinth.Requests, cf.Requests)
	}

	res, err = h.r.MatchOverrides(context.Background(), files[:1])
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Locked) != 0 || strings.Join(res.Kept, ",") != "client-overrides/mods/"+iris.File.Filename || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "requires already has iris") {
		t.Fatalf("a match of a mod requires holds: %+v", res)
	}
}
