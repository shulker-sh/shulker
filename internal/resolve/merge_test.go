package resolve

import (
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func TestMergeFailsOnALocalFileThePackLacks(t *testing.T) {
	dir := t.TempDir()
	p := &project.Project{Dir: dir, Manifest: &manifest.Manifest{Name: "p", Requires: map[string]manifest.Require{}}, Lock: lock.New()}
	pl := lock.New()
	pl.Mods["gone"] = lock.Mod{File: "files/gone.jar"}
	inc := &Incoming{Manifest: &manifest.Manifest{Name: "pack", Requires: map[string]manifest.Require{"gone": {File: "files/gone.jar"}}}, Lock: pl, Dir: t.TempDir()}
	if _, err := Merge(p, inc, []string{"client"}); out.CodeOf(err) != "local-file-missing" {
		t.Fatalf("got %v", err)
	}
}
