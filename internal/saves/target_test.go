package saves

import (
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

func TestTargetAtIsTheGroupOfAShulkerInstanceElseTheDirectory(t *testing.T) {
	roots := Roots{Saves: filepath.Join(t.TempDir(), "saves"), Backups: filepath.Join(t.TempDir(), "backups")}
	dir := t.TempDir()
	in := &config.Instance{ID: "pack", Dir: dir}

	got, err := TargetAt(dir, roots, in, nil, nil)
	if err != nil || got.Group != Default || got.Dir != dir || got.WorldsDir != filepath.Join(roots.Saves, Default) || got.Backups != filepath.Join(roots.Backups, Default) {
		t.Fatalf("an instance without a file joins the default group: %+v %v", got, err)
	}
	f := &instance.File{}
	f.Settings.SavesGroup = "alpha"
	if got, err := TargetAt(dir, roots, in, f, nil); err != nil || got.Group != "alpha" || !got.Home().IsShared {
		t.Fatalf("the instance file names the group: %+v %v", got, err)
	}
	got, err = TargetAt(dir, roots, nil, f, nil)
	if err != nil || got.Group != "" || got.Dir != dir || got.WorldsDir != filepath.Join(dir, "saves") || got.Backups != filepath.Join(dir, instance.Dir, "backups") || got.Home().IsShared {
		t.Fatalf("any other directory keeps its own worlds and backups: %+v %v", got, err)
	}
	f.Settings.SavesGroup = None
	if got, err := TargetAt(dir, roots, in, f, nil); err != nil || got.Group != "" || got.WorldsDir != filepath.Join(dir, "saves") {
		t.Fatalf("group none keeps the instance's own worlds: %+v %v", got, err)
	}
	if group, owned := GroupOf(nil, f); group != None || owned {
		t.Fatalf("no registry row is no group: %q %v", group, owned)
	}
}
