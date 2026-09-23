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
	Renamed    []repairRename    `json:"renamed"`
	Wrote      []string          `json:"wrote"`
	Missing    []string          `json:"missing"`
	total      int
}

type repairRename struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	From string `json:"from"`
	To   string `json:"to"`
}

func (a *app) instancesRepairCmd() *cobra.Command {
	var launcherName, launcherDir string
	cmd := &cobra.Command{
		Use:         "repair",
		Annotations: acts(),
		Short:       "Register instances shulker has lost track of and write any missing instance files",
		Args:        noArgs,
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
	cmd.Flags().StringVar(&launcherDir, "launcher-dir", "", "scan this directory instead of the launcher's own, or instead of the instances root for shulker")
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
	r, err := a.roots()
	if err != nil {
		return repairResult{}, err
	}
	var res repairResult
	instances, loadErr := config.LoadInstances(path)
	if loadErr != nil {
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
		// The launcher's file is the authority on the name, since renaming there is how a player renames
		// an instance; nothing to read keeps the name, which unlink needs once the folder is gone.
		from := in.Name
		if name := launcher.Find(in.Launcher).InstanceName(in.LauncherDir, in.Dir); name != "" {
			in.Name = name
		}
		if in.ID == "" {
			in.ID = uniqueID(instances, inPlaceID(in.Dir), in.Name, in.Dir)
		}
		if in.Name != from {
			res.Renamed = append(res.Renamed, repairRename{ID: in.ID, Dir: in.Dir, From: from, To: in.Name})
		}
		wrote, err := a.repairIntent(*in)
		if err != nil {
			return res, err
		}
		if wrote {
			res.Wrote = append(res.Wrote, instance.Path(in.Dir))
		}
		a.reconcileOrWarn(*in)
	}
	for _, found := range scanLaunchers(launcherName, launcherDir, r.Instances) {
		if registered[filepath.Clean(found.Dir)] {
			continue
		}
		registered[filepath.Clean(found.Dir)] = true
		found.ID = uniqueID(instances, inPlaceID(found.Dir), found.Name, found.Dir)
		wrote, err := a.repairIntent(found)
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
		kept, err := config.WriteInstances(path, instances)
		if err == nil {
			a.printer.Warn("%s; rebuilt it and kept the old one as %s", out.AsError(loadErr).Message, kept)
		}
		return res, err
	}
	_, err = config.UpdateInstances(path, func([]config.Instance) []config.Instance { return instances })
	return res, err
}

// warnReplaced warns that a managed file shulker couldn't read was written over, naming where the
// old one was kept.
func (a *app) warnReplaced(unreadable error, kept string) {
	a.printer.Warn("%s; replaced it and kept the old one as %s", out.AsError(unreadable).Message, kept)
}

// repairIntent writes the instance file for a directory shulker synced before it kept one, from
// what the build recorded. An instance that is a project gets a defaults-only file: its manifest
// holds what it follows, so anything written here could only go stale against it. A file it can't
// read is kept as instance.json.replaced.
func (a *app) repairIntent(in config.Instance) (bool, error) {
	_, loadErr := instance.Load(in.Dir)
	if loadErr == nil {
		return false, nil
	}
	f := instance.New()
	if _, _, inPlace := inPlaceIntent(in.Dir); !inPlace {
		st, _ := build.ReadState(in.Dir)
		source := st.Source
		if source == "" {
			source = in.Source
		}
		if source == "" {
			return false, nil
		}
		f.Source, f.Ref, f.Path, f.Side = source, st.Ref, st.Path, st.Side
	}
	kept, err := f.Replace(in.Dir)
	if kept != "" {
		a.warnReplaced(loadErr, kept)
	}
	return true, err
}

// inPlaceID is the id an instance that is a project was linked under: link makes the manifest's name
// and the id one value, and the folder the launcher named after it is a different one.
func inPlaceID(dir string) string {
	m, _, inPlace, err := inPlaceManifest(dir)
	if err != nil || !inPlace {
		return ""
	}
	return m.Name
}

// scanLaunchers looks where each launcher keeps its instances, and for shulker's own that is the
// instances root. A shulker row records no launcher directory, the same as the row link writes.
func scanLaunchers(only, dir, instancesRoot string) []config.Instance {
	var found []config.Instance
	for _, e := range launcher.All {
		if only != "" && e.Name != only {
			continue
		}
		launcherDir := dir
		switch {
		case launcherDir != "":
		case e.Name == "shulker":
			launcherDir = instancesRoot
		case e.DefaultDir == nil:
			continue
		default:
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
			in.Launcher = e.Name
			if e.Name != "shulker" {
				in.LauncherDir = launcherDir
			}
			if name := e.InstanceName(launcherDir, gameDir); name != "" {
				in.Name = name
			} else if e.IsInstanced {
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
// forgets. The name is the folder's until scanLaunchers reads the one the launcher shows. lastSyncAt
// is when the directory was last built correctly, which a failure after that doesn't undo, so it is
// carried whatever the last sync did. lastError isn't: it belongs to the row shulker is replacing.
func instanceAt(dir string) (config.Instance, bool) {
	f, err := instance.Load(dir)
	if err == nil && f.IsUnlinked {
		return config.Instance{}, false
	}
	// With several modpacks required none of them is the one the instance was linked from, so the
	// manifest says nothing and the sources below answer instead. A directory with no source
	// anywhere is no row shulker can write: the registry needs one.
	pack, _, _ := inPlaceIntent(dir)
	source := pack.Source
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
	for _, rn := range r.Renamed {
		l.OK("renamed "+rn.ID+"  "+l.T.Bump(rn.From, rn.To), "")
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
	case len(r.Registered) == 0 && len(r.Renamed) == 0 && len(r.Wrote) == 0 && len(r.Missing) == 0:
		l.Info("Every instance is registered and has its instance file.")
	}
}
