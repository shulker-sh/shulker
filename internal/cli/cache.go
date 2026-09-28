package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
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
			would, err := r.Prune(d.Cache, true)
			if err != nil {
				return err
			}
			res := cacheInfo{Usage: usage, Roots: r.Count(), Instances: r.Instances, Project: r.Project, LockFiles: r.LockFiles, Locks: len(r.Locks), Prunable: would.Bytes, Unreadable: r.Unreadable}
			for _, problem := range r.Unreadable {
				a.printer.Warn("%s", problem)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.Heading("Cache " + usage.Dir)
				rows := []out.Row{
					{Text: out.HumanBytes(usage.Bytes) + ", " + out.Count(usage.Objects, "object", "objects")},
					{Text: out.Sentence(rootsText(r))},
				}
				if usage.Listings > 0 {
					rows = append(rows, out.Row{Text: out.Count(usage.Listings, "listing pair", "listing pairs") + " in the listing index"})
				}
				switch {
				case would.Empty():
					rows = append(rows, out.Row{Text: "nothing to prune"})
				case would.Bytes == 0:
					rows = append(rows, out.Row{Text: prunedAside(would) + " prunable"})
				default:
					rows = append(rows, out.Row{Text: out.HumanBytes(would.Bytes) + " prunable (" + prunedAside(would) + ")"})
				}
				l.Tree(rows...)
				if !would.Empty() && len(r.Unreadable) == 0 {
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
			pruned, err := r.Prune(d.Cache, false)
			if err != nil {
				return err
			}
			left, err := d.Cache.Usage()
			if err != nil {
				return err
			}
			return a.printer.Emit(pruned, func(l *out.Lines) {
				switch {
				case pruned.Empty() && r.Count() == 0:
					l.Info("Nothing to prune")
					return
				case pruned.Empty():
					l.Info("Nothing to prune; everything in the cache is " + rootsText(r))
					return
				}
				if pruned.Bytes == 0 {
					l.OK("Dropped "+prunedAside(pruned), "")
					return
				}
				l.OK("Freed "+out.HumanBytes(pruned.Bytes)+" of unused cache data", "")
				if left.Bytes > 0 {
					l.Tree(out.Row{Text: out.HumanBytes(left.Bytes) + " of cache data left"})
				}
			})
		},
	}
	lockFlag(cmd, &named)
	return cmd
}

func (a *app) cacheRoots(named []string) (build.Roots, error) {
	d, err := a.deps()
	if err != nil {
		return build.Roots{}, err
	}
	entries, err := a.loadInstanceEntries()
	if err != nil {
		return build.Roots{}, err
	}
	dir := a.dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return build.Roots{}, err
		}
	}
	return build.CacheRoots(d.Cache, entries, dir, named)
}

// rootsText says what keeps the cache's files: "used by 36 instances and this project".
func rootsText(r build.Roots) string {
	var parts []string
	if r.Instances > 0 {
		parts = append(parts, out.Count(r.Instances, "instance", "instances"))
	}
	if r.Project {
		parts = append(parts, "this project")
	}
	if r.LockFiles > 0 {
		parts = append(parts, out.Count(r.LockFiles, "lock file", "lock files"))
	}
	switch len(parts) {
	case 0:
		return "used by nothing: no instance is registered and this is not a project"
	case 1:
		return "used by " + parts[0]
	}
	return "used by " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
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
		{p.Listings, "unused listing pair", "unused listing pairs"},
	} {
		if kind.n > 0 {
			parts = append(parts, out.Count(kind.n, kind.one, kind.many))
		}
	}
	return strings.Join(parts, ", ")
}
