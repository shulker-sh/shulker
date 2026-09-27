package sync

import (
	"errors"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/saves"
)

// OwnedInstance is dir's registry row when shulker launches the instance there, and its instance
// file when it has one; any other directory gets nil for both.
func OwnedInstance(e *Env, dir string) (*config.Instance, *instance.File, error) {
	in, ok := e.registered(dir)
	if !ok || !launcher.Shulker.Launches(in) {
		return nil, nil, nil
	}
	f, err := instance.Load(dir)
	if errors.Is(err, instance.ErrNotFound) {
		return &in, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &in, f, nil
}

// TargetOfDir is the saves target of dir, looked up through the registry, the instance file and
// the manifest that builds dir in place.
func TargetOfDir(e *Env, dir string) (saves.Target, error) {
	in, f, err := OwnedInstance(e, dir)
	if err != nil {
		return saves.Target{}, err
	}
	m, _, inPlace, err := project.InPlace(dir)
	if err != nil {
		return saves.Target{}, err
	}
	if !inPlace {
		m = nil
	}
	return saves.TargetAt(dir, e.Saves, in, f, m)
}

// BackupSource is where target's worlds are, and what the zip comment records about them: the
// instance registered at its directory and the platform its last build installed, when there are
// any. A build backs up before it records its own platform, so this is what the worlds were
// played on.
func BackupSource(e *Env, target saves.Target) saves.Source {
	src := saves.Source{Dir: target.WorldsDir}
	if target.World != "" {
		src.Only = []string{target.World}
	}
	if target.Dir == "" {
		return src
	}
	if in, ok := e.registered(target.Dir); ok {
		src.Instance = in.ID
	}
	state := instance.LoadState(target.Dir)
	src.Minecraft, src.Loader, src.LoaderVersion = state.Minecraft, state.Loader, state.LoaderVersion
	return src
}

// zipping is the step line saves.Take shows for each world, under a warning when a running game
// has the world open.
func (e *Env) zipping(verb string) func(world string, open bool) {
	return func(world string, open bool) {
		if open {
			e.Warn("%s is open in a running game; its backup may be torn", world)
		}
		e.Log("%s %s", verb, world)
	}
}

// beforeModChange is what a build runs before it changes dir's mod set: the automatic backup of
// its worlds, found through the saves target the directory belongs to and kept to
// play.saveBackups. A target that can't be found is a warning, not a failed build.
func (e *Env) beforeModChange(reason, dir string) func() error {
	if reason == "" {
		return nil
	}
	return func() error {
		skip := func(err error) error {
			e.Warn("couldn't back up the worlds in %s before the mods changed: %v", dir, err)
			return nil
		}
		if _, err := e.instances(); err != nil {
			return skip(err)
		}
		target, err := TargetOfDir(e, dir)
		if err != nil {
			return skip(err)
		}
		if e.backedUp == nil {
			e.backedUp = map[saves.Home]bool{}
		}
		_, warning, err := saves.Auto(BackupSource(e, target), target.Home(), reason, e.SaveBackups, e.backedUp, e.zipping("backing up world"))
		if warning != "" {
			e.Warn("%s", warning)
		}
		return err
	}
}

// linkSaves points a shulker instance's saves/ at its save group. Any other directory keeps its
// own worlds, and gets nil.
func (e *Env) linkSaves(dir string) (*saves.Result, error) {
	in, f, err := OwnedInstance(e, dir)
	if err != nil {
		return nil, err
	}
	group, owned := saves.GroupOf(in, f)
	if !owned {
		return nil, nil
	}
	res, err := saves.Link(dir, e.Saves.Saves, group)
	if err != nil {
		return nil, err
	}
	if res.Conflict != "" {
		e.Warn("%s", res.Conflict)
	}
	return &res, nil
}
