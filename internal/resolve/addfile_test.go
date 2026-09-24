package resolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestAddFileCoremodTakesItsLockedKey(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "llibrary-core.jar")
	if err := os.WriteFile(jar, zipBytes(t, "META-INF/MANIFEST.MF", "Manifest-Version: 1.0\nFMLCorePlugin: x.Core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{
		Dir:      dir,
		Manifest: &manifest.Manifest{Requires: map[string]manifest.Require{}},
		Lock:     &lock.Lock{Minecraft: "1.12.2", Loader: lock.Loader{Type: "forge", Version: "14.23.5.2860"}, Mods: map[string]lock.Mod{"llibrary-core": {RequiredBy: []string{"llibrary"}}}},
		Cache:    &cache.Cache{Dir: t.TempDir()},
	}
	if err := r.addFile(context.Background(), jar, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := r.Lock.JarID("llibrary-core"); got != "llibrary-core" {
		t.Errorf("jar id %q", got)
	}
}
