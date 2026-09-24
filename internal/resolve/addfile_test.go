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

func TestIsSlugIsNoURLPathOrDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "base"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.mrpack"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		arg  string
		want bool
	}{
		{"sodium", true},
		{"base", false},
		{"pack.mrpack", false},
		{"other.mrpack", false},
		{"packs/sodium", false},
		{".", false},
		{"https://example.com/pack.git", false},
	} {
		if got := IsSlug(tc.arg, dir); got != tc.want {
			t.Errorf("IsSlug(%q) = %v, want %v", tc.arg, got, tc.want)
		}
	}
}
