package cli

import (
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// instanceText phrases an entry's status for the instances list.
type instanceText struct{ project.InstanceEntry }

func (a *app) instancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "instances",
		Annotations: reads(),
		Short:       "List the instances shulker keeps in sync",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownSubcommand(cmd, args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			entries, err := a.loadInstanceEntries()
			if err != nil {
				return err
			}
			project.SortInstances(entries)
			return a.printer.Emit(entries, func(l *out.Lines) { printInstanceEntries(l, entries) })
		},
	}
	cmd.AddCommand(a.instancesRepairCmd())
	return cmd
}

func (a *app) loadInstances() ([]config.Instance, error) {
	path, err := a.registryFile()
	if err != nil {
		return nil, err
	}
	return config.LoadInstances(path)
}

func (a *app) loadInstanceEntries() ([]project.InstanceEntry, error) {
	instances, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	entries := make([]project.InstanceEntry, len(instances))
	for i, in := range instances {
		entries[i] = project.Inspect(in)
	}
	return entries, nil
}

func printInstanceEntries(l *out.Lines, entries []project.InstanceEntry) {
	if len(entries) == 0 {
		l.Info("Nothing is linked yet; `shulker link prism` adds an instance.")
		return
	}
	var group []out.Entry
	flush := func(name string) {
		if len(group) > 0 {
			l.Entries(launcher.Title(name), group)
			group = nil
		}
	}
	for i, e := range entries {
		if i > 0 && e.Launcher != entries[i-1].Launcher {
			flush(entries[i-1].Launcher)
			l.Blank()
		}
		detail := "from " + e.Source
		if e.Name != "" && e.Name != e.ID {
			detail = e.Name + ", from " + e.Source
		}
		if e.Ref != "" {
			detail += ", ref " + e.Ref
		}
		if e.Path != "" {
			detail += ", path " + e.Path
		}
		if e.Side != "" {
			detail += ", side " + e.Side
		}
		group = append(group, out.Entry{Synced: e.Status == project.StatusSynced && e.LastError == "" && e.LaunchError == "", Name: e.ID, Tag: e.Side, Aside: instanceText{e}.statusText(), Path: e.Dir, Detail: detail, Note: instanceText{e}.launchText()})
	}
	flush(entries[len(entries)-1].Launcher)
}

// launchText is the line under a directory whose last launch never got as far as running the game.
// It is its own line because the reason is an operating system message, too long for the aside.
func (i instanceText) launchText() string {
	if i.LaunchError == "" {
		return ""
	}
	return "last launch didn't start: " + i.LaunchError
}

func (i instanceText) statusText() string {
	switch i.Status {
	case project.StatusMissing:
		return "directory is missing"
	case project.StatusUnreadable:
		return "can't read the directory"
	}
	text := i.syncedText()
	if i.LastError != "" {
		text += ", last sync failed: " + i.LastError
	}
	return text
}

func (i instanceText) syncedText() string {
	if i.Status == project.StatusNotSynced {
		return "not synced yet"
	}
	if i.Problem != "" {
		return i.Problem
	}
	if t, err := time.Parse(time.RFC3339, i.SyncedAt); err == nil {
		return "synced " + t.Local().Format("2006-01-02 15:04")
	}
	return "synced"
}

func (a *app) configFile() (string, error) {
	if a.configPath != "" {
		return a.configPath, nil
	}
	return config.Path()
}

func (a *app) registryFile() (string, error) {
	path, err := a.configFile()
	if err != nil {
		return "", err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return "", err
	}
	return config.RegistryPath(path, cfg), nil
}

func (a *app) registerInstance(in config.Instance) {
	a.updateInstances(func(instances []config.Instance) []config.Instance {
		if i, ok := config.FindInstance(instances, in.Dir); ok {
			if in.ID == "" {
				in.ID = instances[i].ID
			}
			instances[i] = in
			return instances
		}
		in.ID = config.InstanceID(instances, in.ID, in.Name, in.Dir)
		return append(instances, in)
	})
	a.reconcileOrWarn(in)
}

// reconcileOrWarn brings an instance's hooks in line with its instance.json. A launcher shulker
// couldn't set up is worth saying so about, but never worth failing the command that registered it.
func (a *app) reconcileOrWarn(in config.Instance) {
	if err := a.reconcileInstance(in); err != nil {
		a.printer.Warn("hooks not set up for %q: %v", in.Label(), err)
	}
}

// refreshRegistered refreshes the row for a directory a sync just built. A sync adds no row of its
// own, so a directory without one is a detached build and gets no hooks either.
func (a *app) refreshRegistered(in config.Instance) {
	found := false
	a.updateInstances(func(instances []config.Instance) []config.Instance {
		i, ok := config.FindInstance(instances, in.Dir)
		if !ok {
			return instances
		}
		found = true
		in = launcher.RefreshRow(instances[i], in)
		instances[i] = in
		return instances
	})
	if found {
		a.reconcileOrWarn(in)
	}
}

func (a *app) updateInstances(update func([]config.Instance) []config.Instance) bool {
	path, err := a.registryFile()
	if err == nil {
		var changed bool
		if changed, err = config.UpdateInstances(path, update); err == nil {
			return changed
		}
	}
	a.printer.Warn("registry not updated: %v", err)
	return false
}

// checkID refuses an --as value that isn't a valid key, or that another instance already holds.
func (a *app) checkID(as, dir string) error {
	if as == "" {
		return nil
	}
	if !manifest.IsValidKey(as) {
		return out.Errorf("usage", "--as must be lowercase letters, digits, dots, dashes or underscores, up to 64 characters, not %q", as)
	}
	instances, err := a.loadInstances()
	if err != nil {
		return err
	}
	if heldBy, taken := config.IDTaken(instances, as, dir); taken {
		e := out.Errorf("instance-id-taken", "another instance is already called %s (%s)", as, heldBy)
		e.Help = "pass a different --as"
		return e
	}
	return nil
}
