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
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

type linkEntry struct {
	config.Link
	Status   string `json:"status"`
	SyncedAt string `json:"syncedAt,omitempty"`
}

const (
	linkSynced     = "synced"
	linkNotSynced  = "not-synced"
	linkMissing    = "missing"
	linkUnreadable = "unreadable"
)

func (a *app) linksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "links",
		Short: "List linked launcher instances and synced directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			links, err := a.loadLinks()
			if err != nil {
				return err
			}
			res := make([]linkEntry, len(links))
			for i, l := range links {
				res[i] = inspectLink(l)
			}
			sortLinkEntries(res)
			return a.printer.Emit(res, func(l *out.Lines) { printLinkEntries(l, res) })
		},
	}
}

func (a *app) loadLinks() ([]config.Link, error) {
	path, err := a.registryFile()
	if err != nil {
		return nil, err
	}
	return config.LoadLinks(path)
}

func inspectLink(l config.Link) linkEntry {
	e := linkEntry{Link: l, Status: linkSynced}
	if _, err := os.Stat(l.Dir); errors.Is(err, os.ErrNotExist) {
		e.Status = linkMissing
		return e
	} else if err != nil {
		e.Status = linkUnreadable
		return e
	}
	if _, err := os.Stat(filepath.Join(l.Dir, build.StateFile)); errors.Is(err, os.ErrNotExist) {
		e.Status = linkNotSynced
		return e
	} else if err != nil {
		e.Status = linkUnreadable
		return e
	}
	e.SyncedAt = build.LoadState(l.Dir).BuiltAt
	return e
}

func compareLinks(x, y config.Link) int {
	if d := launcher.Rank(x.Launcher) - launcher.Rank(y.Launcher); d != 0 {
		return d
	}
	if c := strings.Compare(x.Launcher, y.Launcher); c != 0 {
		return c
	}
	if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
		return c
	}
	return strings.Compare(x.Dir, y.Dir)
}

func sortLinkEntries(entries []linkEntry) {
	slices.SortStableFunc(entries, func(x, y linkEntry) int { return compareLinks(x.Link, y.Link) })
}

func printLinkEntries(l *out.Lines, entries []linkEntry) {
	if len(entries) == 0 {
		l.Info("Nothing is linked yet; `shulker link prism` or `shulker sync --into <dir>` adds an entry.")
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
		from := "from " + e.Source
		if e.Ref != "" {
			from += ", ref " + e.Ref
		}
		group = append(group, out.Entry{Synced: e.Status == linkSynced, Name: e.Name, Tag: e.Side, Aside: e.statusText(), Path: e.Dir, Detail: from + ", target " + e.Target})
	}
	flush(entries[len(entries)-1].Launcher)
}

func (e linkEntry) statusText() string {
	switch e.Status {
	case linkMissing:
		return "directory is missing"
	case linkUnreadable:
		return "can't read the directory"
	case linkNotSynced:
		return "not synced yet"
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

func (a *app) registerLink(l config.Link) {
	a.updateLinks(func(links []config.Link) []config.Link {
		if i, ok := config.FindLink(links, l.Dir); ok {
			links[i] = l
			return links
		}
		return append(links, l)
	})
}

// registerSync keeps the launcher of an entry that a link made, and its name unless l has one.
// An entry with no launcher is matched against the launcher layouts, so a directory registered
// before shulker recorded one, or synced into by hand, still lands under its launcher.
func (a *app) registerSync(l config.Link, defaultName string) (config.Link, bool) {
	changed := a.updateLinks(func(links []config.Link) []config.Link {
		i, ok := config.FindLink(links, l.Dir)
		if !ok {
			if l.Name == "" {
				l.Name = defaultName
			}
			l.Launcher, l.LauncherDir = launcher.Detect(l.Dir)
			return append(links, l)
		}
		old := links[i]
		l.Launcher, l.LauncherDir = old.Launcher, old.LauncherDir
		if l.Launcher == "" {
			l.Launcher, l.LauncherDir = launcher.Detect(l.Dir)
		}
		if l.Name == "" {
			l.Name = old.Name
		}
		links[i] = l
		return links
	})
	return l, changed
}

func (a *app) updateLinks(update func([]config.Link) []config.Link) bool {
	path, err := a.registryFile()
	if err == nil {
		var changed bool
		if changed, err = config.UpdateLinks(path, update); err == nil {
			return changed
		}
	}
	a.printer.Warn("registry not updated: %v", err)
	return false
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
