package saves

import (
	"errors"
	"path/filepath"
	"slices"
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

func TestPicksShareOneTargetBetweenTheInstancesOfASaveGroup(t *testing.T) {
	shared := Target{Group: "survival", Dir: "/i/a", WorldsDir: "/saves/survival", Backups: "/backups/survival"}
	alone := Target{Group: "creative", Dir: "/i/c", WorldsDir: "/saves/creative", Backups: "/backups/creative"}
	own := Target{Dir: "/i/d", WorldsDir: "/i/d/saves", Backups: "/i/d/.shulker/backups"}
	failed := errors.New("no instance")
	picks := Picks([]Reach{
		{ID: "a", Target: shared},
		{ID: "b", Target: Target{Group: "survival", Dir: "/i/b", WorldsDir: "/saves/survival", Backups: "/backups/survival"}},
		{ID: "c", Target: alone},
		{ID: "d", Target: own},
		{ID: "e", Err: failed},
	})
	if len(picks) != 4 {
		t.Fatalf("picks = %+v", picks)
	}
	if g := picks[0]; g.Group != "survival" || g.Dir != "" || !slices.Equal(g.Instances, []string{"a", "b"}) {
		t.Fatalf("a group reached twice names no directory: %+v", g)
	}
	if c := picks[1]; c.Dir != "/i/c" || !slices.Equal(c.Instances, []string{"c"}) {
		t.Fatalf("a group reached once keeps its instance: %+v", c)
	}
	if d := picks[2]; d.Dir != "/i/d" || !slices.Equal(d.Instances, []string{"d"}) {
		t.Fatalf("an instance with its own saves is its own pick: %+v", d)
	}
	if e := picks[3]; e.Err != failed || !slices.Equal(e.Instances, []string{"e"}) || e.Backups != "" {
		t.Fatalf("a failed reach is its own pick with no target: %+v", e)
	}
}
