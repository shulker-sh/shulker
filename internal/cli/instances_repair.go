package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

type repairResult struct {
	Rebuilt    bool              `json:"rebuilt"`
	Registered []config.Instance `json:"registered"`
	Wrote      []string          `json:"wrote"`
	Missing    []string          `json:"missing"`
	total      int
}

func (a *app) instancesRepairCmd() *cobra.Command {
	var launcherName, launcherDir string
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Register instances shulker has lost track of and write any missing instance files",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if launcherName != "" && launcher.Find(launcherName) == nil {
				return out.Errorf("usage", "--launcher must be %s, not %q", launcher.NameList(), launcherName)
			}
			if launcherDir != "" && launcherName == "" {
				return out.Errorf("usage", "--launcher-dir needs --launcher, so shulker knows whose directory it is")
			}
			res, err := a.repairInstances(launcherName, launcherDir)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, res.print)
		},
	}
	cmd.Flags().StringVar(&launcherName, "launcher", "", "only scan this launcher: "+launcher.NameList())
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "scan this directory instead of the launcher's own")
	return cmd
}

// repairInstances is the one command that tolerates a registry it can't read: it rewrites the file
// from the instances it finds. A registered directory that is gone is reported, never dropped,
// because an unmounted volume looks exactly like a deleted instance and `unlink` is what forgets.
func (a *app) repairInstances(launcherName, launcherDir string) (repairResult, error) {
	path, err := a.registryFile()
	if err != nil {
		return repairResult{}, err
	}
	var res repairResult
	instances, err := config.LoadInstances(path)
	if err != nil {
		a.printer.Warn("%s: %s; rebuilding it", path, out.AsError(err).Message)
		instances, res.Rebuilt = nil, true
	}
	registered := map[string]bool{}
	for _, in := range instances {
		registered[filepath.Clean(in.Dir)] = true
	}
	for i := range instances {
		in := &instances[i]
		if _, err := os.Stat(in.Dir); errors.Is(err, os.ErrNotExist) {
			res.Missing = append(res.Missing, in.Dir)
			continue
		}
		if in.ID == "" {
			in.ID = uniqueID(instances, "", in.Name, in.Dir)
		}
		wrote, err := repairIntent(*in)
		if err != nil {
			return res, err
		}
		if wrote {
			res.Wrote = append(res.Wrote, instance.Path(in.Dir))
		}
		a.reconcileOrWarn(*in)
	}
	for _, found := range scanLaunchers(launcherName, launcherDir) {
		if registered[filepath.Clean(found.Dir)] {
			continue
		}
		registered[filepath.Clean(found.Dir)] = true
		found.ID = uniqueID(instances, "", found.Name, found.Dir)
		wrote, err := repairIntent(found)
		if err != nil {
			return res, err
		}
		if wrote {
			res.Wrote = append(res.Wrote, instance.Path(found.Dir))
		}
		instances = append(instances, found)
		res.Registered = append(res.Registered, found)
	}
	res.total = len(instances)
	if res.Rebuilt {
		return res, config.WriteInstances(path, instances)
	}
	_, err = config.UpdateInstances(path, func([]config.Instance) []config.Instance { return instances })
	return res, err
}

// repairIntent writes the instance file for a directory shulker synced before it kept one, from
// what the build recorded. An instance that is a project gets a defaults-only file: its manifest
// holds what it follows, so anything written here could only go stale against it.
func repairIntent(in config.Instance) (bool, error) {
	if _, err := instance.Load(in.Dir); err == nil {
		return false, nil
	}
	f := instance.New()
	if _, _, _, inPlace := inPlaceIntent(in.Dir); !inPlace {
		st, _ := build.ReadState(in.Dir)
		source := st.Source
		if source == "" {
			source = in.Source
		}
		if source == "" {
			return false, nil
		}
		f.Source, f.Ref, f.Side = source, st.Ref, st.Side
	}
	return true, f.Save(in.Dir)
}

func scanLaunchers(only, dir string) []config.Instance {
	var found []config.Instance
	for _, e := range launcher.All {
		if only != "" && e.Name != only {
			continue
		}
		launcherDir := dir
		if launcherDir == "" {
			if e.DefaultDir == nil {
				continue
			}
			d, err := e.DefaultDir()
			if err != nil {
				continue
			}
			launcherDir = d
		}
		for _, gameDir := range e.GameDirs(launcherDir) {
			in, ok := instanceAt(gameDir)
			if !ok {
				continue
			}
			in.Launcher, in.LauncherDir = e.Name, launcherDir
			if e.Instanced {
				in.Name = filepath.Base(e.InstanceDir(gameDir))
			}
			found = append(found, in)
		}
	}
	return found
}

// instanceAt recognises a directory shulker syncs from what it holds, in order: an in-place
// shulker.json, the manifest of a project that is an instance under ADR 0001, whose one modpack
// entry says what it follows; the instance file's source; the state a build left before instance
// files existed. An instance file saying unlinked wins over all three, because unlink is what
// forgets. The name is never the manifest's: the row's name is the one the launcher shows, which
// scanLaunchers reads from the launcher itself. lastSyncAt is when the directory was last built
// correctly, which a failure after that doesn't undo, so it is carried whatever the last sync did.
// lastError isn't: it belongs to the row shulker is replacing.
func instanceAt(dir string) (config.Instance, bool) {
	f, err := instance.Load(dir)
	if err == nil && f.Unlinked {
		return config.Instance{}, false
	}
	// With several modpacks required none of them is the one the instance was linked from, so the
	// manifest says nothing and the sources below answer instead. A directory with no source
	// anywhere is no row shulker can write: the registry needs one.
	source, _, _, _ := inPlaceIntent(dir)
	if source == "" && err == nil {
		source = f.Source
	}
	if source == "" {
		st, _ := build.ReadState(dir)
		source = st.Source
	}
	if source == "" {
		return config.Instance{}, false
	}
	in := config.Instance{Name: filepath.Base(dir), Dir: dir, Source: source}
	if err == nil && f.Resolved != nil {
		in.LastSync = f.Resolved.LastSyncAt
	}
	return in, true
}

func (r repairResult) print(l *out.Lines) {
	for _, in := range r.Registered {
		l.OKInto("registered "+in.ID, in.Dir, launcher.Title(in.Launcher))
	}
	for _, path := range r.Wrote {
		l.OK("wrote "+path, "")
	}
	if len(r.Missing) > 0 {
		l.Info(plural(len(r.Missing), "instance directory is", "instance directories are") + " missing; they are kept in case the disk holding them is away")
		for _, dir := range r.Missing {
			l.Tree(out.Row{Text: dir})
		}
		l.Nudge("To forget one", "shulker unlink <id>")
	}
	switch {
	case r.total == 0:
		l.Info("Nothing is linked yet; `shulker link prism` adds an instance.")
	case len(r.Registered) == 0 && len(r.Wrote) == 0 && len(r.Missing) == 0:
		l.Info("Every instance is registered and has its instance file.")
	}
}
