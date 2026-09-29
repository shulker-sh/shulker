package cli

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

// instanceText phrases an entry's status for the instances list.
type instanceText struct{ project.InstanceEntry }

func (a *app) instancesCmd() *cobra.Command {
	var verbose bool
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
			return a.printer.Emit(entries, func(l *out.Lines) { printInstanceEntries(l, entries, verbose) })
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also print each instance's path, source and ref.")
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
// side and status. A launch that never started puts its reason on the status cell's second line.
// Verbose adds the directory, wrapped in a link so it copies, and the source and ref.
func printInstanceEntries(l *out.Lines, entries []project.InstanceEntry, verbose bool) {
	if len(entries) == 0 {
		l.Info("Nothing is linked yet.")
		l.Nudge("Link a pack into a launcher", "shulker link <launcher>")
		return
	}
	t := l.T
	headers := []string{"", "Instance", "Launcher", "Side", "Status"}
	if verbose {
		headers = append(headers, "Path", "Source", "Ref")
	}
	rows := make([][]string, len(entries))
	statusWidth := 0
	for i, e := range entries {
		rows[i] = []string{t.GlyphDot(), e.ID, launcher.Title(e.Launcher), e.Side, instanceText{e}.statusText()}
		if verbose {
			rows[i] = append(rows[i], t.Link(out.Tilde(e.Dir), e.Dir), out.Tilde(e.Source), e.Ref)
		}
		statusWidth = max(statusWidth, out.Width(rows[i][4]))
	}
	// The note folds to the statuses' own width, so the path stays the column that gives way.
	for i, e := range entries {
		if note := (instanceText{e}).launchText(); note != "" {
			rows[i][4] += "\n" + t.Grey(ansi.Wrap(note, statusWidth, "/-"))
		}
	}
	l.Table(headers, rows, func(row, col int) lipgloss.Style {
		e := entries[row]
		switch {
		case col == 0:
			if e.Status == project.StatusSynced && e.LastError == "" && e.LaunchError == "" {
				return t.StyleGreen().Bold(true)
			}
			return t.StyleYellow().Bold(true)
		case col == 1:
			return t.StyleBold()
		case col == 2 || col > 4:
			return t.StyleGrey()
		case col == 3:
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
		return "synced " + out.Ago(t)
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

func (a *app) reconcileOrWarn(in config.Instance) (rehooked bool) {
	se, err := a.syncEnv()
	if err != nil {
		a.printer.Warn("hooks not set up for %s: %v", launcher.Named(in), err)
		return false
	}
	return sync.Reconcile(se, in)
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
