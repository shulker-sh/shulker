package resolve

import (
	"os"
	"path/filepath"
	"testing"

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
