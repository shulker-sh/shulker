package cli

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
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

// printInstanceEntries is one table across launchers: the sync dot, the id bold, the launcher,
// side and status, then the full directory so it copies, wrapped in a link. A launch that never
// started puts its reason on the status cell's second line. Source and ref are left to --json.
func printInstanceEntries(l *out.Lines, entries []project.InstanceEntry) {
	if len(entries) == 0 {
		l.Info("Nothing is linked yet; `shulker link prism` adds an instance.")
		return
	}
	t := l.T
	rows := make([][]string, len(entries))
	statusWidth := 0
	for i, e := range entries {
		rows[i] = []string{t.GlyphDot(), e.ID, e.Launcher, e.Side, instanceText{e}.statusText(), t.Link(e.Dir, e.Dir)}
		statusWidth = max(statusWidth, out.Width(rows[i][4]))
	}
	// The note folds to the statuses' own width, so the path stays the column that gives way.
	for i, e := range entries {
		if note := (instanceText{e}).launchText(); note != "" {
			rows[i][4] += "\n" + t.Grey(ansi.Wrap(note, statusWidth, "/-"))
		}
	}
	l.Table([]string{"", "Instance", "Launcher", "Side", "Status", "Path"}, rows, func(row, col int) lipgloss.Style {
		e := entries[row]
		switch col {
		case 0:
			if e.Status == project.StatusSynced && e.LastError == "" && e.LaunchError == "" {
				return t.StyleGreen().Bold(true)
			}
			return t.StyleYellow().Bold(true)
		case 1:
			return t.StyleBold()
		case 2, 5:
			return t.StyleGrey()
		case 3:
			return t.StyleCyan()
		}
		return t.Style()
	})
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

// reconcileOrWarn brings an instance's hooks in line with its instance.json: a run left open by a
// watcher that was killed is closed first, which is true of every instance, then a launcher with a
// slot gets launcher.Reconcile and a warning for each command it adopted. A launcher shulker
// couldn't set up is worth saying so about, but never worth failing the command that registered it.
func (a *app) reconcileOrWarn(in config.Instance) {
	if err := instance.ReconcileRuns(in.Dir); err != nil {
		a.printer.Warn("%v", err)
	}
	e := launcher.Find(in.Launcher)
	if e == nil || e.Slot == nil {
		// A plain synced directory has no slot to fill, so it gets no scripts either.
		return
	}
	var adopted []string
	f, err := instance.Load(in.Dir)
	if err == nil {
		var exe string
		if exe, err = launcher.ShulkerPath(); err == nil {
			adopted, err = launcher.Reconcile(e, in, f, exe)
		}
	}
	for _, command := range adopted {
		a.warnUnreproducible(*e.Slot, command)
	}
	if err != nil {
		a.printer.Warn("hooks not set up for %q: %v", in.Label(), err)
	}
}

// warnUnreproducible reports the tokens an adopted command uses that shulker can't reproduce,
// because they come from the launcher's own Java resolution rather than from the instance.
func (a *app) warnUnreproducible(slot launcher.Slot, command string) {
	var named []string
	for _, token := range slot.Unreproducible {
		if strings.Contains(command, "$"+token) {
			named = append(named, "$"+token)
		}
	}
	if len(named) > 0 {
		a.printer.Warn("the command shulker adopted uses %s, which only the launcher can fill in, so it will be empty when shulker runs it", strings.Join(named, " and "))
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
