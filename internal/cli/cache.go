package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
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
	Clones     bool     `json:"clones"`
}

func (a *app) cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and trim the downloads shulker shares between projects",
	}
	cmd.AddCommand(a.cacheInfoCmd(), a.cacheVerifyCmd(), a.cachePruneCmd())
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
			would, err := r.Prune(d.Cache, cache.PruneOptions{DryRun: true})
			if err != nil {
				return err
			}
			dirs, err := a.roots()
			if err != nil {
				return err
			}
			res := cacheInfo{Usage: usage, Roots: r.Count(), Instances: r.Instances, Project: r.Project, LockFiles: r.LockFiles, Locks: len(r.Locks), Prunable: would.Bytes, Unreadable: r.Unreadable, Clones: d.Cache.ClonesInto(dirs.Instances)}
			for _, problem := range r.Unreadable {
				a.printer.Warn("%s", problem)
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.Heading("Cache " + usage.Dir)
				rows := []out.Row{
					{Text: out.HumanBytes(usage.Bytes) + " in " + out.Count(usage.Objects, "object", "objects")},
					{Text: out.Sentence(rootsText(r))},
				}
				if usage.Manual > 0 {
					rows = append(rows, out.Row{Text: out.Count(usage.Manual, "manual download", "manual downloads") + ", kept by prune"})
				}
				rows = append(rows, out.Row{Text: "Listing index: " + out.Count(usage.Listings, "pair", "pairs")})
				switch {
				case would.Empty():
					rows = append(rows, out.Row{Text: "Nothing can be freed"})
				case would.Bytes == 0:
					rows = append(rows, out.Row{Text: out.Sentence(prunedAside(would)) + " can be dropped"})
				default:
					rows = append(rows, out.Row{Text: out.HumanBytes(would.Bytes) + " can be freed (" + prunedAside(would) + ")"})
				}
				l.Tree(rows...)
				l.Info(cloneText(res.Clones, d.Cache.NoClone, dirs.Instances))
				if !would.Empty() && len(r.Unreadable) == 0 {
					l.Nudge("Free it", "shulker cache prune")
				}
			})
		},
	}
	a.dirFlag(cmd)
	lockFlag(cmd, &named)
	return cmd
}

func cloneText(clones, off bool, instances string) string {
	switch {
	case off:
		return "Builds copy cached files: cache.clone is off"
	case clones:
		return "Builds clone cached files into " + out.Tilde(instances) + ", taking no extra space"
	default:
		return "Builds copy cached files: " + out.Tilde(instances) + " can't clone from the cache"
	}
}

func lockFlag(cmd *cobra.Command, named *[]string) {
	cmd.Flags().StringArrayVar(named, "lock", nil, "also keep what this lock file references; repeat for more.")
}

func (a *app) cachePruneCmd() *cobra.Command {
	var named []string
	var manual bool
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
			pruned, err := r.Prune(d.Cache, cache.PruneOptions{Manual: manual})
			if err != nil {
				return err
			}
			left, err := d.Cache.Usage()
			if err != nil {
				return err
			}
			return a.printer.Emit(pruned, func(l *out.Lines) {
				if pruned.KeptManual > 0 {
					defer l.Info("Kept " + out.Count(pruned.KeptManual, "manual download", "manual downloads") + " still in use; the cache is " + rootsText(r) + ".")
				}
				switch {
				case pruned.Empty() && r.Count() == 0:
					l.Info("Nothing to prune")
					return
				case pruned.Empty():
					l.Info("Nothing to prune; everything in the cache is " + rootsText(r) + ".")
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
	a.dirFlag(cmd)
	lockFlag(cmd, &named)
	cmd.Flags().BoolVar(&manual, "manual", false, "also remove manual downloads, which nothing can fetch again.")
	return cmd
}

func (a *app) cacheVerifyCmd() *cobra.Command {
	var named []string
	var fix bool
	cmd := &cobra.Command{
		Use:         "verify",
		Annotations: decides(),
		Short:       "Check every cached file against its hash and its provider",
		Long:        "Check every cached file against its hash and its provider: rehash every object, ask Modrinth and CurseForge whether they still have each file that an instance, the project here or any of their history entries locks, and list the objects none of them uses. It fails on a changed object or a file gone from its provider. --fix drops the changed objects, so the next build downloads them again.",
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
			for _, problem := range r.Unreadable {
				a.printer.Warn("%s", problem)
			}
			if fix {
				a.logActing()
			}
			v, err := r.Verify(cmd.Context(), d.Cache, d.Providers, fix)
			if err != nil {
				return err
			}
			if !v.Fails() {
				return a.printer.Emit(v, func(l *out.Lines) { printCacheCheck(l, v, d.Providers) })
			}
			if !a.printer.JSON {
				printCacheCheck(a.printer.Out(), v, d.Providers)
			}
			return cacheVerifyFailed(v)
		},
	}
	a.dirFlag(cmd)
	lockFlag(cmd, &named)
	cmd.Flags().BoolVar(&fix, "fix", false, "drop the changed objects, so the next build downloads them again.")
	return cmd
}

// cacheVerifyFailed sums up what failed the check, each already printed in full above it.
func cacheVerifyFailed(v *build.CacheCheck) error {
	var parts []string
	if n := len(v.Changed); n > 0 && !v.Dropped {
		parts = append(parts, changedHeadline(n))
	}
	if n := len(v.Takedowns); n > 0 {
		parts = append(parts, goneHeadline(n))
	}
	if n := len(v.Moved); n > 0 {
		parts = append(parts, movedHeadline(n))
	}
	e := out.Errorf("cache-verify-failed", "%s", strings.Join(parts, "; "))
	e.Data, e.IsSummary = v, true
	if !v.Dropped {
		for _, o := range v.Changed {
			e.Items = append(e.Items, o.Sha512)
		}
	}
	for _, f := range slices.Concat(v.Takedowns, v.Moved) {
		e.Items = append(e.Items, f.Keys...)
	}
	return e
}

func changedHeadline(n int) string {
	return out.Count(n, "object", "objects") + " in the cache no longer " + countWord(n == 1, "matches its", "match their") + " hash"
}

func movedHeadline(n int) string {
	return out.Count(n, "file is", "files are") + " filed under another project than the lock says"
}

func printCacheCheck(l *out.Lines, v *build.CacheCheck, ps provider.Providers) {
	section := sections(l)
	if len(v.Changed)+len(v.Takedowns)+len(v.Moved) == 0 {
		section()
		checked := "rehashed " + out.Count(v.Objects, "object", "objects") + ", checked takedowns"
		if len(v.Skipped) > 0 {
			checked = "rehashed " + out.Count(v.Objects, "object", "objects")
		}
		l.OK("No problems found", checked)
	}
	printSkipped(l, v.Skipped, ps, section)
	if n := len(v.Changed); n > 0 {
		section()
		if v.Dropped {
			l.OK("Dropped "+out.Count(n, "changed object", "changed objects"), "the next build downloads "+countWord(n == 1, "it", "them")+" again")
		} else {
			l.Failed(changedHeadline(n))
		}
		var rows []out.Row
		for _, o := range v.Changed {
			rows = append(rows, out.Row{Text: o.Sha512[:12] + " (" + out.HumanBytes(o.Size) + ")"})
		}
		l.Tree(rows...)
		if !v.Dropped {
			l.Nudge("Drop "+countWord(n == 1, "it", "them")+" so "+countWord(n == 1, "it downloads", "they download")+" again", "shulker cache verify --fix")
		}
	}
	if n := len(v.Takedowns); n > 0 {
		section()
		l.Failed(goneHeadline(n))
		var rows []out.Row
		for _, f := range v.Takedowns {
			rows = append(rows, out.Row{Label: strings.Join(f.Keys, ", "), Text: ps.Title(f.Provider) + " no longer has version " + cmp.Or(f.Number, f.Version) + ", locked by " + strings.Join(f.Roots, ", ")})
		}
		l.Tree(rows...)
		l.Blank()
		l.Paragraph("A file gone from its provider is a reason to look, not proof: authors delete their own old versions too. Run `shulker audit <key>` where it's locked to look at it, then `update` or `remove` it there.")
	}
	if n := len(v.Moved); n > 0 {
		section()
		l.Failed(movedHeadline(n))
		var rows []out.Row
		for _, f := range v.Moved {
			rows = append(rows, out.Row{Label: strings.Join(f.Keys, ", "), Text: fmt.Sprintf("%s files it under project %s, not %s, locked by %s", ps.Title(f.Provider), f.FiledUnder, f.Project, strings.Join(f.Roots, ", "))})
		}
		l.Tree(rows...)
	}
	if n := len(v.Unused); n > 0 {
		section()
		l.Info(out.Count(n, "object", "objects") + " no instance or project uses, " + out.HumanBytes(v.UnusedBytes))
		l.Nudge("Free "+countWord(n == 1, "it", "them"), "shulker cache prune")
	}
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

// rootsText says what keeps the cache's files: "kept for 36 instances and the project here".
func rootsText(r build.Roots) string {
	var parts []string
	if r.Instances > 0 {
		parts = append(parts, out.Count(r.Instances, "instance", "instances"))
	}
	if r.Project {
		parts = append(parts, "the project here")
	}
	if r.LockFiles > 0 {
		parts = append(parts, out.Count(r.LockFiles, "lock file", "lock files"))
	}
	switch len(parts) {
	case 0:
		return "kept for nothing: no instance is registered and this is not a project"
	case 1:
		return "kept for " + parts[0]
	}
	return "kept for " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
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
