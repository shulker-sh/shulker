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
	LockFiles  int      `json:"lockFiles"`
	Locks      int      `json:"locks"`
	Prunable   int64    `json:"prunable"`
	Unreadable []string `json:"unreadable,omitempty"`
}

// roots are the locks a prune must not strip: every registered instance's and
// the project the command runs in, each with the locks of its history entries,
// and each lock file named with --lock.
// A root whose lock won't load is carried as a problem rather than an error, so
// `cache info` can still report while `cache prune` refuses.
type roots struct {
	locks      []cache.Root
	instances  int
	project    bool
	lockFiles  int
	unreadable []string
}

func (r roots) count() int {
	n := r.instances + r.lockFiles
	if r.project {
		n++
	}
	return n
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
	var named []string
	cmd := &cobra.Command{
		Use:         "info",
		Annotations: reads(),
		Short:       "Show where the cache is, how big it is, and how much prune would free",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			usage, err := d.Cache.Usage()
			if err != nil {
				return err
			}
			r, err := a.cacheRoots(named)
			if err != nil {
				return err
			}
			would, err := d.Cache.Prune(r.locks, true)
			if err != nil {
				return err
			}
			res := cacheInfo{Usage: usage, Roots: r.count(), Instances: r.instances, Project: r.project, LockFiles: r.lockFiles, Locks: len(r.locks), Prunable: would.Bytes, Unreadable: r.unreadable}
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
	lockFlag(cmd, &named)
	return cmd
}

func lockFlag(cmd *cobra.Command, named *[]string) {
	cmd.Flags().StringArrayVar(named, "lock", nil, "also keep what this lock file references; repeat for more")
}

func (a *app) cachePruneCmd() *cobra.Command {
	var named []string
	cmd := &cobra.Command{
		Use:         "prune",
		Annotations: acts(),
		Short:       "Remove cached files no instance or project references",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			r, err := a.cacheRoots(named)
			if err != nil {
				return err
			}
			if len(r.unreadable) > 0 {
				return out.Errorf("cache-root-unreadable", "%s", r.unreadable[0])
			}
			pruned, err := d.Cache.Prune(r.locks, false)
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
	lockFlag(cmd, &named)
	return cmd
}

func (a *app) cacheRoots(named []string) (roots, error) {
	d, err := a.deps()
	if err != nil {
		return roots{}, err
	}
	entries, err := a.loadInstanceEntries()
	if err != nil {
		return roots{}, err
	}
	var r roots
	for _, path := range named {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return roots{}, out.Errorf("lock-not-found", "%s doesn't exist", path)
		} else if err != nil {
			return roots{}, err
		}
		r.lockFiles++
		lk, err := lock.Load(path)
		if err != nil {
			r.unreadable = append(r.unreadable, path+" can't be read, so pruning could remove files it needs ("+out.AsError(err).Message+"); fix it, or leave out its --lock")
			continue
		}
		r.locks = append(r.locks, cache.Root{Lock: lk})
	}
	seen := map[string]bool{}
	for _, in := range entries {
		dir := filepath.Clean(in.Dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		locks, present, unreadable, err := build.CacheRoots(d.Cache, dir, cache.Root{Source: in.Source, Ref: in.Ref, Path: in.Path})
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
	locks, _, unreadable, err := build.CacheRoots(d.Cache, dir, cache.Root{})
	if err != nil {
		return roots{}, err
	}
	r.project = true
	r.locks = append(r.locks, locks...)
	r.unreadable = append(r.unreadable, unreadable...)
	return r, nil
}

func rootsText(r roots) string {
	var parts []string
	if r.instances > 0 {
		parts = append(parts, plural(r.instances, "instance", "instances"))
	}
	if r.project {
		parts = append(parts, "this project")
	}
	if r.lockFiles > 0 {
		parts = append(parts, plural(r.lockFiles, "lock file", "lock files"))
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
