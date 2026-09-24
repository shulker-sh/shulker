package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

func (a *app) matchCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:         "match [path...]",
		Annotations: acts(),
		Short:       "Lock override jars and packs that Modrinth or CurseForge host",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			files, err := a.overrideFiles(p.Dir, args)
			if err != nil {
				return err
			}
			var res *resolve.Matched
			if dryRun {
				r, err := a.resolver(cmd.Context(), p)
				if err != nil {
					return err
				}
				if res, err = r.MatchOverrides(cmd.Context(), files); err != nil {
					return err
				}
				a.warn(res.Warnings)
			} else {
				_, err := a.relockProject(cmd, p, relockOptions{keepUnchanged: true}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
					var err error
					if res, err = r.MatchOverrides(cmd.Context(), files); err != nil {
						return "", err
					}
					a.warn(res.Warnings)
					return "", nil
				})
				if err != nil {
					return err
				}
				for _, rel := range res.Moved {
					if err := os.Remove(filepath.Join(p.Dir, filepath.FromSlash(rel))); err != nil {
						return err
					}
				}
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				title := "matched"
				if dryRun {
					title = "would match"
				}
				l.OK(title, fmt.Sprintf("%s, %s kept as overrides", lockedSummary(a.titles(), res.Locked), plural(len(res.Kept), "file", "files")))
				var rows []out.Row
				for _, f := range res.Locked {
					rows = append(rows, out.Row{Label: "locked", Text: f.ID})
				}
				for _, file := range res.Kept {
					rows = append(rows, out.Row{Label: "kept", Text: file})
				}
				l.Tree(rows...)
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "look the files up and report what would be locked, changing nothing")
	return cmd
}

// matchFolders are the folders of an override layer whose files a provider can host.
var matchFolders = append([]string{"mods", "resourcepacks", "shaderpacks"}, lock.DatapackFolders...)

// overrideFiles reads the jars and pack zips in the project's override layers, or only the named
// ones, each named by its layer and its path within it.
func (a *app) overrideFiles(dir string, named []string) ([]packarchive.Override, error) {
	var rels []string
	var err error
	if len(named) == 0 {
		rels, err = scanOverrides(dir)
	} else {
		rels, err = a.namedOverrides(dir, named)
	}
	if err != nil {
		return nil, err
	}
	files := make([]packarchive.Override, len(rels))
	for i, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		layer, within, _ := strings.Cut(rel, "/")
		files[i] = packarchive.Override{Layer: layer, Path: within, Data: data}
	}
	return files, nil
}

// scanOverrides lists every matchable file in the project's override layers, relative to dir.
func scanOverrides(dir string) ([]string, error) {
	var rels []string
	for _, layer := range packarchive.Layers {
		for _, folder := range matchFolders {
			entries, err := os.ReadDir(filepath.Join(dir, layer, folder))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if rel := layer + "/" + folder + "/" + e.Name(); e.Type().IsRegular() && isMatchable(rel) {
					rels = append(rels, rel)
				}
			}
		}
	}
	return rels, nil
}

// namedOverrides checks that each named path is a matchable file in an override layer, giving it
// relative to dir.
func (a *app) namedOverrides(dir string, named []string) ([]string, error) {
	var rels []string
	for _, arg := range named {
		if !filepath.IsAbs(arg) && a.dir != "" {
			arg = filepath.Join(a.dir, arg)
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(dir, abs)
		if err != nil || !filepath.IsLocal(rel) || !isMatchable(filepath.ToSlash(rel)) {
			e := out.Errorf("override-path", "%s isn't a jar in mods/ or a zip in resourcepacks/, shaderpacks/ or a datapack folder of an override folder", arg)
			e.Help = "match looks up files in overrides/, client-overrides/ and server-overrides/"
			return nil, e
		}
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			return nil, out.Errorf("file-not-found", "%s isn't a file", arg)
		}
		if rel = filepath.ToSlash(rel); !slices.Contains(rels, rel) {
			rels = append(rels, rel)
		}
	}
	return rels, nil
}

func isMatchable(rel string) bool {
	layer, within, _ := strings.Cut(rel, "/")
	return slices.Contains(packarchive.Layers, layer) && (packarchive.IsModJar(within) || packarchive.IsPackZip(within))
}
