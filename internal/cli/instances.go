package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type instanceEntry struct {
	config.Instance
	Status   string `json:"status"`
	SyncedAt string `json:"syncedAt,omitempty"`
	Target   string `json:"target,omitempty"`
	Side     string `json:"side,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Problem  string `json:"problem,omitempty"`
	intent   *instance.File
}

const (
	instanceSynced     = "synced"
	instanceNotSynced  = "not-synced"
	instanceMissing    = "missing"
	instanceUnreadable = "unreadable"
)

func (e instanceEntry) intentPath() string { return filepath.Join(instance.Dir, instance.FileName) }

func (a *app) instancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "List the instances shulker keeps in sync",
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

// inspectInstance reads what the instance directory says about itself. A directory shulker can't
// read, or that holds no instance file, is still listed: `instances repair` is what fixes it.
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
		e.Target, e.Ref = f.Target, f.Ref
		if e.Source == "" {
			e.Source = f.Source
		}
		if f.Resolved != nil {
			e.Side = f.Resolved.Side
		}
	}
	state, _ := build.ReadState(in.Dir)
	if e.intent == nil {
		e.Ref = state.Ref
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
	if e.Target == "" {
		e.Target = state.Target
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
		l.Info("Nothing is linked yet; `shulker link prism` or `shulker sync --into <dir>` adds an instance.")
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
		if e.Target != "" {
			detail += ", target " + e.Target
		}
		group = append(group, out.Entry{Synced: e.Status == instanceSynced, Name: e.ID, Tag: e.Side, Aside: e.statusText(), Path: e.Dir, Detail: detail})
	}
	flush(entries[len(entries)-1].Launcher)
}

func (e instanceEntry) statusText() string {
	switch e.Status {
	case instanceMissing:
		return "directory is missing"
	case instanceUnreadable:
		return "can't read the directory"
	case instanceNotSynced:
		return "not synced yet"
	}
	if e.Problem != "" {
		return e.Problem
	}
	if t, err := time.Parse(time.RFC3339, e.SyncedAt); err == nil {
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
		in.ID = uniqueID(instances, in.ID, in.Name, in.Dir)
		return append(instances, in)
	})
}

// registerSync keeps the launcher of an instance that a link made, and its name unless in has one.
// An instance with no launcher is matched against the launcher layouts, so a directory registered
// before shulker recorded one, or synced into by hand, still lands under its launcher.
func (a *app) registerSync(in config.Instance, defaultName string) (config.Instance, bool) {
	changed := a.updateInstances(func(instances []config.Instance) []config.Instance {
		i, ok := config.FindInstance(instances, in.Dir)
		if !ok {
			if in.Name == "" {
				in.Name = defaultName
			}
			in.Launcher, in.LauncherDir = launcher.Detect(in.Dir)
			in.ID = uniqueID(instances, in.ID, in.Name, in.Dir)
			return append(instances, in)
		}
		old := instances[i]
		in.Launcher, in.LauncherDir = old.Launcher, old.LauncherDir
		if in.Launcher == "" {
			in.Launcher, in.LauncherDir = launcher.Detect(in.Dir)
		}
		if in.Name == "" {
			in.Name = old.Name
		}
		if in.ID == "" {
			in.ID = old.ID
		}
		instances[i] = in
		return instances
	})
	return in, changed
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

// saveIntent writes what a directory syncs from, keeping the settings block a person may have
// edited. Every directory shulker syncs into gets one, launcher instance or not.
func saveIntent(dir, source, ref, target, side string) error {
	f, err := instance.Load(dir)
	switch {
	case errors.Is(err, instance.ErrNotFound):
		f = instance.New(source, ref, target)
	case err != nil:
		return err
	default:
		f.Source, f.Ref, f.Target = source, ref, target
	}
	if f.Resolved == nil {
		f.Resolved = &instance.Resolved{}
	}
	f.Resolved.Side = side
	return f.Save(dir)
}

var unsafeIDChars = regexp.MustCompile(`[^a-z0-9._-]+`)

// slugID is the id derived from an instance's name, matching manifest.ValidKey so it is safe as a
// path segment and as the argument to -i.
func slugID(name string) string {
	slug := strings.Trim(unsafeIDChars.ReplaceAllString(strings.ToLower(name), "-"), "-._")
	for slug != "" && !manifest.ValidKey(slug) {
		slug = slug[1:]
	}
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-._")
	}
	if slug == "" {
		slug = "instance"
	}
	return slug
}

// uniqueID keeps want when it is free, and otherwise suffixes the slug the way history entries
// taken in the same second are suffixed.
func uniqueID(instances []config.Instance, want, name, dir string) string {
	if want == "" {
		want = slugID(name)
	}
	if want == "" {
		want = slugID(filepath.Base(dir))
	}
	taken := func(id string) bool {
		i, ok := config.FindID(instances, id)
		return ok && !sameDir(instances[i].Dir, dir)
	}
	if !taken(want) {
		return want
	}
	for n := 2; ; n++ {
		id := want + "-" + strconv.Itoa(n)
		if !taken(id) {
			return id
		}
	}
}

// checkID refuses an --as value that isn't a valid key, or that another instance already holds.
func (a *app) checkID(as, dir string) error {
	if as == "" {
		return nil
	}
	if !manifest.ValidKey(as) {
		return out.Errorf("usage", "--as must be lowercase letters, digits, dots, dashes or underscores, up to 64 characters, not %q", as)
	}
	instances, err := a.loadInstances()
	if err != nil {
		return err
	}
	if i, ok := config.FindID(instances, as); ok && !sameDir(instances[i].Dir, dir) {
		return out.Errorf("instance-id-taken", "another instance is already called %s (%s); pass a different --as", as, instances[i].Dir)
	}
	return nil
}
