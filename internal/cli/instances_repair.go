package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
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
	res := repairResult{Registered: []config.Instance{}, Renamed: []repairRename{}, Wrote: []string{}, Missing: []string{}}
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
			in.ID = config.InstanceID(instances, project.InPlaceID(in.Dir), in.Name, in.Dir)
		}
		if in.Name != from {
			res.Renamed = append(res.Renamed, repairRename{ID: in.ID, Dir: in.Dir, From: from, To: in.Name})
		}
		rep, err := project.RepairIntent(*in)
		if rep.Kept != "" {
			a.warnReplaced(rep.Unreadable, rep.Kept)
		}
		if err != nil {
			return res, err
		}
		if rep.Wrote {
			res.Wrote = append(res.Wrote, instance.Path(in.Dir))
		}
		a.reconcileOrWarn(*in)
	}
	for _, found := range launcher.Scan(launcherName, launcherDir, r.Instances, project.InstanceAt) {
		if registered[filepath.Clean(found.Dir)] {
			continue
		}
		registered[filepath.Clean(found.Dir)] = true
		found.ID = config.InstanceID(instances, project.InPlaceID(found.Dir), found.Name, found.Dir)
		rep, err := project.RepairIntent(found)
		if rep.Kept != "" {
			a.warnReplaced(rep.Unreadable, rep.Kept)
		}
		if err != nil {
			return res, err
		}
		if rep.Wrote {
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
