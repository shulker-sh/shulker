package saves

import (
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/manifest"
)

// Roots are the folder the save groups live in and the one their backups live in.
type Roots struct {
	Saves   string
	Backups string
}

// Target is whose worlds a saves command acts on: a save group's, or a directory's own. World is
// set for a server, which loads only its level-name world.
type Target struct {
	Group     string `json:"group,omitempty"`
	Dir       string `json:"dir,omitempty"`
	WorldsDir string `json:"worldsDir"`
	World     string `json:"world,omitempty"`
	Backups   string `json:"-"`
}

// Home is where the target's backups go: a group's are shared between its instances.
func (t Target) Home() Home {
	return Home{Dir: t.Backups, IsShared: t.Group != ""}
}

// GroupTarget is the target for a save group by name.
func GroupTarget(r Roots, group string) Target {
	return Target{Group: group, WorldsDir: filepath.Join(r.Saves, group), Backups: filepath.Join(r.Backups, group)}
}

// GroupOf is the group a shulker instance joins, and None for every other directory, since only
// instances shulker launches itself share worlds. in is the instance's registry row when shulker
// launches it, and f its instance file when it has one; owned says whether in was given.
func GroupOf(in *config.Instance, f *instance.File) (group string, owned bool) {
	if in == nil {
		return None, false
	}
	if f == nil || f.Settings.SavesGroup == "" {
		return Default, true
	}
	return f.Settings.SavesGroup, true
}

// TargetAt is the target a directory's worlds belong to: its save group when it is a shulker
// instance in one, and the directory itself otherwise, group None included, with its worlds wherever build.WorldsOf
// finds them and its backups in its own instance folder. m is the manifest that builds dir in
// place, or nil.
func TargetAt(dir string, r Roots, in *config.Instance, f *instance.File, m *manifest.Manifest) (Target, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Target{}, err
	}
	if group, owned := GroupOf(in, f); owned && group != None {
		t := GroupTarget(r, group)
		t.Dir = dir
		return t, nil
	}
	w, err := build.WorldsOf(dir, m)
	if err != nil {
		return Target{}, err
	}
	return Target{Dir: dir, WorldsDir: w.Dir, World: w.Level, Backups: filepath.Join(dir, instance.Dir, "backups")}, nil
}

// Reach is one instance and the target it resolves to, or why it couldn't.
type Reach struct {
	ID     string
	Target Target
	Err    error
}

// Pick is one target a fanned-out command acts on, and the instances that reach it. Err is why
// their target couldn't be resolved.
type Pick struct {
	Target
	Instances []string
	Err       error
}

// Picks is one target per instance, except that the instances of one save group share a single
// pick: reached by one instance it still names that instance's directory, reached by several it
// names none, as --group does. A reach that failed is its own pick.
func Picks(reaches []Reach) []Pick {
	var picks []Pick
	for _, r := range reaches {
		if r.Err != nil {
			picks = append(picks, Pick{Instances: []string{r.ID}, Err: r.Err})
			continue
		}
		i := slices.IndexFunc(picks, func(p Pick) bool { return p.Err == nil && p.Backups == r.Target.Backups })
		if i < 0 {
			picks = append(picks, Pick{Target: r.Target, Instances: []string{r.ID}})
			continue
		}
		picks[i].Instances = append(picks[i].Instances, r.ID)
		picks[i].Dir = ""
	}
	return picks
}
