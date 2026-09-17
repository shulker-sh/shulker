package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type cacheInfo struct {
	cache.Usage
	Roots      int      `json:"roots"`
	Instances  int      `json:"instances"`
	Project    bool     `json:"project"`
	Locks      int      `json:"locks"`
	Prunable   int64    `json:"prunable"`
	Unreadable []string `json:"unreadable,omitempty"`
}

// roots are the locks a prune must not strip: every registered instance's and
// the project the command runs in, each with the locks of its history entries.
// A root whose lock won't load is carried as a problem rather than an error, so
// `cache info` can still report while `cache prune` refuses.
type roots struct {
	locks      []cache.Root
	instances  int
	project    bool
	unreadable []string
}

func (r roots) count() int {
	if r.project {
		return r.instances + 1
	}
	return r.instances
}

func (a *app) cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and trim the downloads shulker shares between projects",
	}
	cmd.AddCommand(a.cacheInfoCmd(), a.cachePruneCmd())
	return cmd
}

func (a *app) cacheInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show where the cache is, how big it is, and how much prune would free",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			usage, err := d.cache.Usage()
			if err != nil {
				return err
			}
			r, err := a.cacheRoots()
			if err != nil {
				return err
			}
			would, err := d.cache.Prune(r.locks, true)
			if err != nil {
				return err
			}
			res := cacheInfo{Usage: usage, Roots: r.count(), Instances: r.instances, Project: r.project, Locks: len(r.locks), Prunable: would.Bytes, Unreadable: r.unreadable}
			for _, problem := range r.unreadable {
				a.printer.Warn("%s", problem)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.Heading("Cache " + usage.Dir)
				rows := []out.Row{
					{Text: out.HumanBytes(usage.Bytes) + ", " + plural(usage.Objects, "object", "objects")},
					{Text: rootsText(r)},
				}
				if would.Empty() {
					rows = append(rows, out.Row{Text: "nothing to prune"})
				} else {
					rows = append(rows, out.Row{Text: out.HumanBytes(would.Bytes) + " prunable (" + prunedAside(would) + ")"})
				}
				l.Tree(rows...)
				if !would.Empty() && len(r.unreadable) == 0 {
					l.Nudge("Free it", "shulker cache prune")
				}
			})
		},
	}
}

func (a *app) cachePruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove cached files no instance or project references",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			r, err := a.cacheRoots()
			if err != nil {
				return err
			}
			if len(r.unreadable) > 0 {
				return out.Errorf("cache-root-unreadable", "%s", r.unreadable[0])
			}
			pruned, err := d.cache.Prune(r.locks, false)
			if err != nil {
				return err
			}
			return a.printer.Emit(pruned, func(l *out.Lines) {
				if pruned.Empty() {
					l.Info("nothing to prune; everything in the cache is referenced by " + rootsText(r))
					return
				}
				l.OK("freed "+out.HumanBytes(pruned.Bytes), prunedAside(pruned))
			})
		},
	}
}

func (a *app) cacheRoots() (roots, error) {
	links, err := a.loadLinks()
	if err != nil {
		return roots{}, err
	}
	var r roots
	seen := map[string]bool{}
	for _, link := range links {
		dir := filepath.Clean(link.Dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		locks, present, unreadable, err := dirRoots(dir, link.Source, link.Ref)
		if err != nil {
			return roots{}, err
		}
		if !present {
			continue
		}
		r.instances++
		r.locks = append(r.locks, locks...)
		r.unreadable = append(r.unreadable, unreadable...)
	}
	dir := a.dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return roots{}, err
		}
	}
	dir = filepath.Clean(dir)
	if seen[dir] {
		return r, nil
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err != nil {
		return r, nil
	}
	locks, _, unreadable, err := dirRoots(dir, "", "")
	if err != nil {
		return roots{}, err
	}
	r.project = true
	r.locks = append(r.locks, locks...)
	r.unreadable = append(r.unreadable, unreadable...)
	return r, nil
}

// dirRoots reads the locks one directory keeps alive: its own and one per
// history entry. A directory that is gone contributes nothing, since the
// instance it held was deleted; one whose lock is there but can't be read is
// returned as a problem, which stops a prune because it may be an instance that
// still needs its files, but leaves an inspection free to report.
func dirRoots(dir, source, ref string) ([]cache.Root, bool, []string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil, nil
	} else if err != nil {
		return nil, false, nil, err
	}
	dirs := []string{dir}
	// A separate-dir instance keeps its lock in the project it was built from,
	// not in the game directory the registry records.
	if src := filepath.Clean(source); src != "." && src != dir {
		if _, err := os.Stat(filepath.Join(src, manifest.FileName)); err == nil {
			dirs = append(dirs, src)
		}
	}
	var paths []string
	for _, d := range dirs {
		paths = append(paths, filepath.Join(d, lock.FileName))
		entries, err := build.History(d)
		if err != nil {
			return nil, false, nil, err
		}
		for _, e := range entries {
			paths = append(paths, filepath.Join(build.HistoryPath(d), e.ID, lock.FileName))
		}
	}
	var found []cache.Root
	var unreadable []string
	for _, path := range paths {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, false, nil, err
		}
		lk, err := lock.Load(path)
		if err != nil {
			unreadable = append(unreadable, path+" can't be read, so pruning could remove files it needs ("+out.AsError(err).Message+"); fix it, or run `shulker unlink` for that instance")
			continue
		}
		found = append(found, cache.Root{Lock: lk, Source: source, Ref: ref})
	}
	return found, true, unreadable, nil
}

func rootsText(r roots) string {
	var parts []string
	if r.instances > 0 {
		parts = append(parts, plural(r.instances, "instance", "instances"))
	}
	if r.project {
		parts = append(parts, "this project")
	}
	if len(parts) == 0 {
		return "no roots: no instance is registered and this is not a project"
	}
	return plural(r.count(), "root", "roots") + " (" + strings.Join(parts, ", ") + ")"
}

func prunedAside(p cache.Pruned) string {
	var parts []string
	for _, kind := range []struct {
		n         int
		one, many string
	}{
		{p.Files, "file", "files"},
		{p.Checkouts, "modpack checkout", "modpack checkouts"},
		{p.Installs, "loader install", "loader installs"},
		{p.Logs, "installer log", "installer logs"},
		{p.Temp, "leftover file", "leftover files"},
	} {
		if kind.n > 0 {
			parts = append(parts, plural(kind.n, kind.one, kind.many))
		}
	}
	return strings.Join(parts, ", ")
}
