package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"shulker.sh/shulker/internal/out"
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
			var rels []string
			if len(args) == 0 {
				rels, err = project.ScanOverrides(p.Dir)
			} else {
				rels, err = a.namedOverrides(p.Dir, args)
			}
			if err != nil {
				return err
			}
			files, err := project.MatchableOverrides(p.Dir, rels)
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
				_, err := a.relockOpened(cmd, p, relockOptions{keepUnchanged: true}, func(_ *project.Project, r *resolve.Resolver) (string, error) {
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
				title := "Matched"
				if dryRun {
					title = "Would match"
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
		rel, ok := project.OverrideRel(dir, abs)
		if !ok {
			e := out.Errorf("override-path", "%s isn't a jar in mods/ or a zip in resourcepacks/, shaderpacks/ or a datapack folder of an override folder", arg)
			e.Help = "match looks up files in overrides/, client-overrides/ and server-overrides/"
			return nil, e
		}
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			return nil, out.Errorf("file-not-found", "%s isn't a file", arg)
		}
		if !slices.Contains(rels, rel) {
			rels = append(rels, rel)
		}
	}
	return rels, nil
}
