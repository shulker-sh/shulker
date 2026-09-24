package cli

import (
	"errors"
	"os"
	"path/filepath"

	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

type instanceEntry struct {
	config.Instance
	Status        string `json:"status"`
	SyncedAt      string `json:"syncedAt,omitempty"`
	Side          string `json:"side,omitempty"`
	AssumesClient bool   `json:"assumeClient,omitempty"`
	Ref           string `json:"ref,omitempty"`
	Path          string `json:"path,omitempty"`
	Problem       string `json:"problem,omitempty"`
	LaunchError   string `json:"launchError,omitempty"`
	intent        *instance.File
	detached      bool
}

const (
	instanceSynced     = "synced"
	instanceNotSynced  = "not-synced"
	instanceMissing    = "missing"
	instanceUnreadable = "unreadable"
)

func (i instanceEntry) intentPath() string { return filepath.Join(instance.Dir, instance.FileName) }

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
			sortInstanceEntries(entries)
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

func (a *app) loadInstanceEntries() ([]instanceEntry, error) {
	instances, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	entries := make([]instanceEntry, len(instances))
	for i, in := range instances {
		entries[i] = inspectInstance(in)
	}
	return entries, nil
}

// inspectInstance reads what the instance directory says about itself: its manifest where it is a
// project, its instance file otherwise, and what the last build recorded where there is neither. A
// directory shulker can't read is still listed: `instances repair` is what fixes it.
func inspectInstance(in config.Instance) instanceEntry {
	e := instanceEntry{Instance: in, Status: instanceSynced}
	if _, err := os.Stat(in.Dir); errors.Is(err, os.ErrNotExist) {
		e.Status = instanceMissing
		return e
	} else if err != nil {
		e.Status = instanceUnreadable
		return e
	}
	switch f, err := instance.Load(in.Dir); {
	case errors.Is(err, instance.ErrNotFound):
		e.Problem = "no " + e.intentPath()
	case err != nil:
		e.Problem = out.AsError(err).Message
	default:
		e.intent = f
		e.Side, e.Ref, e.Path, e.AssumesClient = f.Side, f.Ref, f.Path, f.AssumesClient
		if e.Source == "" {
			e.Source = f.Source
		}
	}
	if records := instance.LoadLaunches(in.Dir); len(records) > 0 {
		if last := records[len(records)-1]; last.Outcome == instance.OutcomeNotStarted {
			e.LaunchError = last.Error
		}
	}
	pack, side, inPlace := project.InPlaceIntent(in.Dir)
	state, _ := build.ReadState(in.Dir)
	switch {
	case inPlace:
		e.Ref, e.Path, e.Side = pack.Ref, pack.Path, side
		if pack.Source != "" {
			e.Source = pack.Source
		}
	case e.intent == nil:
		e.Ref, e.Path = state.Ref, state.Path
		if e.Source == "" {
			e.Source = state.Source
		}
	}
	if _, err := os.Stat(build.StatePath(in.Dir)); errors.Is(err, os.ErrNotExist) {
		e.Status = instanceNotSynced
		return e
	} else if err != nil {
		e.Status = instanceUnreadable
		return e
	}
	e.SyncedAt = state.BuiltAt
	if e.Side == "" {
		e.Side = state.Side
	}
	return e
}

func compareInstances(x, y instanceEntry) int {
	if d := launcher.Rank(x.Launcher) - launcher.Rank(y.Launcher); d != 0 {
		return d
	}
	if c := strings.Compare(x.Launcher, y.Launcher); c != 0 {
		return c
	}
	if c := strings.Compare(strings.ToLower(x.Label()), strings.ToLower(y.Label())); c != 0 {
		return c
	}
	return strings.Compare(x.Dir, y.Dir)
}

func sortInstanceEntries(entries []instanceEntry) {
	slices.SortStableFunc(entries, compareInstances)
}

func printInstanceEntries(l *out.Lines, entries []instanceEntry) {
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
		group = append(group, out.Entry{Synced: e.Status == instanceSynced && e.LastError == "" && e.LaunchError == "", Name: e.ID, Tag: e.Side, Aside: e.statusText(), Path: e.Dir, Detail: detail, Note: e.launchText()})
	}
	flush(entries[len(entries)-1].Launcher)
}

// launchText is the line under a directory whose last launch never got as far as running the game.
// It is its own line because the reason is an operating system message, too long for the aside.
func (i instanceEntry) launchText() string {
	if i.LaunchError == "" {
		return ""
	}
	return "last launch didn't start: " + i.LaunchError
}

func (i instanceEntry) statusText() string {
	switch i.Status {
	case instanceMissing:
		return "directory is missing"
	case instanceUnreadable:
		return "can't read the directory"
	}
	text := i.syncedText()
	if i.LastError != "" {
		text += ", last sync failed: " + i.LastError
	}
	return text
}

func (i instanceEntry) syncedText() string {
	if i.Status == instanceNotSynced {
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

// refreshRegistered keeps a row that a link or a repair wrote in step with the directory it points
// at: its source, and the launcher of a row written before shulker recorded one. A sync adds no
// row of its own, so a directory without one is a detached build and gets no hooks either.
func (a *app) refreshRegistered(in config.Instance) {
	found := false
	a.updateInstances(func(instances []config.Instance) []config.Instance {
		i, ok := config.FindInstance(instances, in.Dir)
		if !ok {
			return instances
		}
		found = true
		old := instances[i]
		in.ID, in.Name = old.ID, old.Name
		in.Launcher, in.LauncherDir = old.Launcher, old.LauncherDir
		in.LastSync, in.LastError = old.LastSync, old.LastError
		if in.Launcher == "" {
			in.Launcher, in.LauncherDir = launcher.Detect(in.Dir)
		}
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
