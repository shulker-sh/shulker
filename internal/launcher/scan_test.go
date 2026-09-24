package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
)

func TestScanNamesShulkersOwnInstancesAfterTheirFolder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"smp", "creative"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	recognise := func(dir string) (config.Instance, bool) {
		if filepath.Base(dir) == "creative" {
			return config.Instance{}, false
		}
		return config.Instance{Dir: dir, Source: "src"}, true
	}
	found := Scan("shulker", "", root, recognise)
	if len(found) != 1 {
		t.Fatalf("found %+v", found)
	}
	in := found[0]
	if in.Launcher != "shulker" || in.LauncherDir != "" || in.Name != "smp" || in.Dir != filepath.Join(root, "smp") || in.Source != "src" {
		t.Fatalf("row = %+v", in)
	}
	if got := Scan("prism", "", root, recognise); len(got) != 0 {
		t.Fatalf("only scans the named launcher: %+v", got)
	}
}
