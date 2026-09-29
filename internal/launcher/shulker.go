package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/manifest"
)

// Shulker is shulker's own instances: folders under its instances root that it builds, keeps
// in step and launches itself. Its launcher directory is that root, so it has no --launcher-dir and
// fills no slot.
var Shulker = &Entry{
	Name: "shulker", Title: "Shulker", IsInstanced: true, gameDirIsInstance: true,
	Usage: Usage{
		Short: "Create an instance shulker owns and launches itself",
		Noun:  "instance",
		As:    "nickname for this instance, which names its folder and finds it with -i (default: from the pack's name)",
		Force: "repoint the modpack an instance already follows.",
	},
	relink: relinkShulker, forget: forgetShulker, name: shulkerName, gameDirs: instanceGameDirs,
	place: placeShulker, link: linkShulker,
}

// Owned lists the instances shulker owns for the project at source. Instances other launchers
// own are synced from it too, but never count, since shulker doesn't start them; nor does the
// project directory itself.
func Owned(registry []config.Instance, source string) []config.Instance {
	var own []config.Instance
	for _, in := range registry {
		if Shulker.Launches(in) && config.SameDir(in.Source, source) && !config.SameDir(in.Dir, source) {
			own = append(own, in)
		}
	}
	return own
}

// relinkShulker rebuilds an instance shulker owns. It names the instance with --as rather than
// --name, because the id is also the folder under the instances root, and it carries no side: a
// shulker instance is a client.
func relinkShulker(e *Entry, l Linked) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	if l.Path != "" {
		args = append(args, "--path", shellArg(l.Path))
	}
	return append(args, "--as", shellArg(l.ID)), ""
}

// forgetShulker has nothing to take away. Shulker runs its own hooks in process, so no slot holds a
// command and no script was generated; unlinking is the registry row going and nothing else.
func forgetShulker(e *Entry, l config.Instance) (Forgotten, error) {
	return unlinked(e, l, kept), nil
}

// shulkerName is the display name link recorded, since shulker is the launcher that shows it and a
// shulker instance is always a project building where it stands.
func shulkerName(_ *Entry, _, gameDir string) string {
	m, err := manifest.Load(filepath.Join(gameDir, manifest.FileName))
	if err != nil {
		return ""
	}
	return m.DisplayName("client")
}

func instanceGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(launcherDir, func(dir string) []string {
		return []string{dir}
	})
}

// placeShulker puts the instance under the instances root by its nickname, which is its registry
// id too.
func placeShulker(_ *Entry, req *Link) (Placement, error) {
	nick := shulkerNick(req.Registry, req.LauncherDir, req.ID, req.Name)
	dir := filepath.Join(req.LauncherDir, nick)
	return Placement{ID: nick, Dir: dir, GameDir: dir}, nil
}

// linkShulker writes nothing of its own: the project the link leaves in the folder is the whole
// instance.
func linkShulker(_ context.Context, _ *Entry, _ *Link, p Placement) (InstanceResult, error) {
	res := InstanceResult{Dir: p.Dir, GameDir: p.Dir, Key: p.ID}
	if _, err := os.Stat(p.Dir); errors.Is(err, os.ErrNotExist) {
		res.Created = true
	}
	return res, nil
}

// shulkerNick is the nickname a shulker instance goes by. It names the folder under the instances
// root as well as the registry id, so the two can never drift and a relink finds its own instance
// by name. Without --as it is a slug of the pack's display name, suffixed until it is either free
// or already the folder it names.
func shulkerNick(instances []config.Instance, root, as, display string) string {
	if as != "" {
		return as
	}
	want := config.SlugID(display)
	for n := 1; ; n++ {
		id := want
		if n > 1 {
			id = want + "-" + strconv.Itoa(n)
		}
		if i, ok := config.FindID(instances, id); !ok || config.SameDir(instances[i].Dir, filepath.Join(root, id)) {
			return id
		}
	}
}
