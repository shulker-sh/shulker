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
// what the build recorded.
func repairIntent(in config.Instance) (bool, error) {
	if _, err := instance.Load(in.Dir); err == nil {
		return false, nil
	}
	st, _ := build.ReadState(in.Dir)
	source, ref, side := st.Source, st.Ref, st.Side
	if source == "" {
		source = in.Source
	}
	if source == "" {
		return false, nil
	}
	f := instance.New()
	f.Source, f.Ref, f.Side = source, ref, side
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

// instanceAt recognises a directory shulker syncs by its .shulker/ directory: the instance file it
// keeps now, or the state a build left before instance files existed. lastSyncAt is when the
// directory was last built correctly, which a failure after that doesn't undo, so it is carried
// whatever the last sync did. lastError isn't: it belongs to the row shulker is replacing.
func instanceAt(dir string) (config.Instance, bool) {
	in := config.Instance{Name: filepath.Base(dir), Dir: dir}
	if f, err := instance.Load(dir); err == nil {
		in.Source = f.Source
		// An instance that is a project follows the one modpack its manifest requires. With several
		// required none of them is the one, and what the last build recorded is all there is.
		if source, _, _, inPlace := inPlaceIntent(dir); inPlace {
			if source == "" {
				st, _ := build.ReadState(dir)
				source = st.Source
			}
			in.Source = source
		}
		if r := f.Resolved; r != nil {
			in.LastSync = r.LastSyncAt
		}
		return in, !f.Unlinked
	}
	st, err := build.ReadState(dir)
	if err != nil || st.Source == "" {
		return config.Instance{}, false
	}
	in.Source = st.Source
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
		l.Info("Nothing is linked yet; `shulker link prism` or `shulker sync --into <dir>` adds an instance.")
	case len(r.Registered) == 0 && len(r.Wrote) == 0 && len(r.Missing) == 0:
		l.Info("Every instance is registered and has its instance file.")
	}
}
