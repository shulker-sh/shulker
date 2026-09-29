package build

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/security"
	"shulker.sh/shulker/internal/takedown"
)

// UsedBy names, for each of the files asked about by sha512, the registered instances whose lock
// holds it. A warning asks for it only when there is something to warn about, since it reads
// every instance's lock.
type UsedBy func(sha512s []string) map[string][]string

// InstancesUsing is UsedBy over instances: each instance's own lock, not its history entries.
func InstancesUsing(c *cache.Cache, instances []project.InstanceEntry, sha512s []string) map[string][]string {
	used := map[string][]string{}
	seen := map[string]bool{}
	for _, in := range instances {
		at := filepath.Clean(in.Dir)
		if seen[at] {
			continue
		}
		seen[at] = true
		roots, _, _, err := dirRoots(c, at, cache.Root{Source: in.Source, Ref: in.Ref, Path: in.Path, Name: in.Label()})
		if err != nil {
			continue
		}
		for _, root := range roots {
			if root.Name != in.Label() {
				continue
			}
			held := map[string]bool{}
			for _, e := range takedown.Entries(root.Lock) {
				held[e.Sha512] = true
			}
			for _, sha := range sha512s {
				if held[sha] && !slices.Contains(used[sha], root.Name) {
					used[sha] = append(used[sha], root.Name)
				}
			}
		}
	}
	return used
}

// recordedTakedowns are the files check found that l still locks, under the same key.
func recordedTakedowns(check *instance.Takedowns, l *lock.Lock) []takedown.File {
	if check == nil {
		return nil
	}
	locked := map[string]bool{}
	for _, e := range takedown.Entries(l) {
		locked[e.Key+"/"+e.Sha512] = true
	}
	var files []takedown.File
	for _, f := range check.Files {
		if locked[f.Key+"/"+f.Sha512] {
			files = append(files, f)
		}
	}
	return files
}

// TakedownWarning warns about locked files gone from their provider or filed under another
// project, naming the instances that use each and what to do about them. The cached copies are
// still placed: a file gone is a reason to look, not proof.
func TakedownWarning(files []takedown.File, ps provider.Providers, usedBy UsedBy) out.SecurityWarning {
	var shas, keys []string
	for _, f := range files {
		shas = append(shas, f.Sha512)
		if !slices.Contains(keys, f.Key) {
			keys = append(keys, f.Key)
		}
	}
	var used map[string][]string
	if usedBy != nil {
		used = usedBy(shas)
	}
	var rows []string
	for _, f := range files {
		row := f.Key + ": " + ps.Title(f.Provider) + " no longer has the locked version, " + cmp.Or(f.Number, f.Version)
		if f.Status == takedown.Moved {
			row = fmt.Sprintf("%s: %s files the locked version under project %s, not %s", f.Key, ps.Title(f.Provider), f.FiledUnder, f.Project)
		}
		if names := used[f.Sha512]; len(names) > 0 {
			row += " (used by " + strings.Join(names, ", ") + ")"
		}
		rows = append(rows, row)
	}
	them, copies := "them", "copies"
	if len(files) == 1 {
		them, copies = "it", "copy"
	}
	headline := out.Count(len(files), "locked file is gone from its provider", "locked files are gone from their provider") + " or filed under another project; shulker still places the cached " + copies + "."
	named := strings.Join(keys, " ")
	return security.Warn(security.Takedowns, headline+"\n"+strings.Join(rows, "\n"), files,
		out.Nudge{Lead: "Look at " + them, Command: "shulker audit " + named},
		out.Nudge{Lead: "Move off " + them, Command: "shulker update " + named},
		out.Nudge{Lead: "Or remove " + them, Command: "shulker remove " + named},
	)
}
