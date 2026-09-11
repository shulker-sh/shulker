package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
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

var launcherOrder = []string{"prism", "multimc", "mojang"}

var launcherTitles = map[string]string{"prism": "Prism Launcher", "multimc": "MultiMC", "mojang": "Minecraft Launcher"}

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
			return a.printer.Emit(res, func(w io.Writer) { printLinkEntries(w, res) })
		},
	}
}

func (a *app) loadLinks() ([]config.Link, error) {
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadFile(path)
	return cfg.Links, err
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

func launcherRank(launcher string) int {
	if launcher == "" {
		return len(launcherOrder) + 1
	}
	if i := slices.Index(launcherOrder, launcher); i >= 0 {
		return i
	}
	return len(launcherOrder)
}

func compareLinks(x, y config.Link) int {
	if d := launcherRank(x.Launcher) - launcherRank(y.Launcher); d != 0 {
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

func launcherTitle(launcher string) string {
	if launcher == "" {
		return "Other directories"
	}
	if t, ok := launcherTitles[launcher]; ok {
		return t
	}
	return launcher
}

func printLinkEntries(w io.Writer, entries []linkEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "Nothing is linked yet; `shulker link prism` or `shulker sync --into <dir>` adds an entry.")
		return
	}
	for i, e := range entries {
		if i == 0 || e.Launcher != entries[i-1].Launcher {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, launcherTitle(e.Launcher))
		}
		fmt.Fprintf(w, "  %s (%s), %s\n", e.Name, e.Side, e.statusText())
		fmt.Fprintf(w, "    %s\n", e.Dir)
		from := "    from " + e.Source
		if e.Ref != "" {
			from += ", ref " + e.Ref
		}
		fmt.Fprintf(w, "%s, target %s\n", from, e.Target)
	}
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
func (a *app) registerSync(l config.Link, defaultName string) (config.Link, bool) {
	changed := a.updateLinks(func(links []config.Link) []config.Link {
		i, ok := config.FindLink(links, l.Dir)
		if !ok {
			if l.Name == "" {
				l.Name = defaultName
			}
			return append(links, l)
		}
		old := links[i]
		l.Launcher, l.LauncherDir = old.Launcher, old.LauncherDir
		if l.Name == "" {
			l.Name = old.Name
		}
		links[i] = l
		return links
	})
	return l, changed
}

func (a *app) updateLinks(update func([]config.Link) []config.Link) bool {
	path, err := a.configFile()
	if err == nil {
		var changed bool
		if changed, err = config.UpdateLinks(path, update); err == nil {
			return changed
		}
	}
	a.progress("warning: config.json not updated: %v", err)
	return false
}
