package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
)

// buildDirs is the build directory plus every directory this side was synced
// into: the links registry first, then any left in shulker.local.json by a
// shulker that recorded them there. Directories that are gone are skipped.
func (a *app) buildDirs(p *project.Project, lf *local.File, side string) (string, []string, error) {
	buildDir, err := filepath.Abs(filepath.Join(p.Dir, p.Manifest.BuildDir(side)))
	if err != nil {
		return "", nil, err
	}
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(buildDir)
	entries, err := a.loadInstanceEntries()
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if e.Side == side && isSameDir(e.Source, p.Dir) {
			add(e.Dir)
		}
	}
	for _, d := range lf.ExistingSyncDirs(side) {
		add(d)
	}
	return buildDir, dirs, nil
}

func (a *app) diffCmd() *cobra.Command {
	var into string
	cmd := &cobra.Command{
		Use:         "diff [side]",
		Annotations: reads(),
		Short:       "Show build files that differ from what build would write",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			sides, err := projectSides(p, args)
			if err != nil {
				return err
			}
			if into != "" {
				if len(sides) != 1 {
					return ambiguousSide(sides, "")
				}
				if into, err = filepath.Abs(into); err != nil {
					return err
				}
			}
			var reports []*build.DiffReport
			where := map[*build.DiffReport]string{}
			for _, side := range sides {
				buildDir, dirs := "", []string{into}
				if into == "" {
					if buildDir, dirs, err = a.buildDirs(p, lf, side); err != nil {
						return err
					}
					if len(dirs) == 0 {
						dirs = []string{""}
					}
				}
				for _, dir := range dirs {
					rep, err := b.Diff(side, build.Options{Dir: dir, Features: lf.Features})
					if err != nil && dir != "" && dir != buildDir && into == "" {
						a.printer.Warn("skipped %s: %v", dir, err)
						continue
					}
					if err != nil {
						return err
					}
					a.warnFor(side, len(sides) > 1, rep.Warnings)
					reports = append(reports, rep)
					where[rep] = "the build directory"
					if dir != "" && dir != buildDir {
						where[rep] = dir
					}
				}
			}
			return a.printer.Emit(reports, func(l *out.Lines) {
				for _, rep := range reports {
					if len(rep.Files) == 0 {
						l.OK("no changes in "+rep.Side, where[rep])
						continue
					}
					l.Heading(rep.Side + " " + l.T.Grey(fmt.Sprintf("(%s in %s)", plural(len(rep.Files), "file changed", "files changed"), where[rep])))
					for i, f := range rep.Files {
						if i > 0 {
							l.Blank()
						}
						l.Items(out.Item{Kind: diffKind(f.State), Name: f.Path, Aside: []string{diffAside(f.State)}})
						l.Diff(f.Diff)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "directory the side was synced into (default: the build directory and every directory it was synced into)")
	return cmd
}

func (a *app) pullCmd() *cobra.Command {
	var side, into, to, as string
	var keys []string
	cmd := &cobra.Command{
		Use:         "pull [file...]",
		Annotations: acts(),
		Short:       "Copy edits made in a build directory back into their source",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkPullAs(as, to, args); err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			side, err := singleSide(p, side)
			if err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			lf, err := local.Load(p.Dir)
			if err != nil {
				return err
			}
			if into != "" {
				if into, err = filepath.Abs(into); err != nil {
					return err
				}
			} else if into, err = a.pullSource(b, p, lf, side, args); err != nil {
				return err
			}
			rep, err := b.Pull(side, build.PullRequest{Files: args, Keys: keys, To: to}, build.Options{Dir: into, Features: lf.Features})
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			if rep.ManifestChanged {
				if err := p.SaveManifest(); err != nil {
					return err
				}
			}
			if err := a.adoptFiles(cmd, p, rep, as); err != nil {
				return err
			}
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OK("pulled "+rep.Side, fmt.Sprintf("%s, %s written to shulker.json, %d skipped", plural(len(rep.Pulled)+len(rep.Entries), "file", "files"), plural(len(rep.Keys), "key", "keys"), len(rep.Skipped)))
				var rows []out.Row
				arrow := " " + l.T.ArrowBump() + " "
				for _, f := range rep.Pulled {
					rows = append(rows, out.Row{Label: "pulled", Text: strings.ReplaceAll(f, " -> ", arrow)})
				}
				for _, k := range rep.Keys {
					rows = append(rows, out.Row{Label: "set", Text: k})
				}
				for _, k := range slices.Concat(rep.Entries, rep.Adopted) {
					rows = append(rows, out.Row{Label: "adopted", Text: strings.ReplaceAll(k, " -> ", arrow)})
				}
				for _, s := range rep.Skipped {
					rows = append(rows, out.Row{Label: "skipped", Text: s})
				}
				l.Tree(rows...)
			})
		},
	}
	cmd.Flags().StringVar(&side, "side", "", "side whose build directory to pull from (default: the only declared side)")
	cmd.Flags().StringVar(&into, "into", "", "directory the side was synced into (default: whichever of the build directory and its sync directories has edits)")
	cmd.Flags().StringArrayVar(&keys, "key", nil, "start managing this key of the named .properties file; repeat for more")
	cmd.Flags().StringVar(&as, "as", "", "requires key for the one named jar or pack adopted as a file entry (default: a jar's mod id, a pack file's name)")
	cmd.Flags().StringVar(&to, "to", "", "override folder to write into: client, server, or a feature name (default: where the file already lives, or overrides/ for a new one)")
	return cmd
}

func checkPullAs(as, to string, files []string) error {
	switch {
	case as == "":
		return nil
	case len(files) != 1:
		return out.Errorf("usage", "--as needs exactly one file")
	case !manifest.IsValidKey(as):
		return out.Errorf("usage", "--as takes up to 64 lowercase letters, digits, dots, dashes and underscores, starting with a letter or digit, not %q", as)
	case to != "" || build.AdoptedType(filepath.ToSlash(filepath.Clean(files[0]))) == "":
		return out.Errorf("usage", "--as applies to a jar in mods/ or a zip in resourcepacks/ or shaderpacks/, which pull adopts as a file entry")
	}
	return nil
}

// adoptFiles adds each jar and pack that pull set aside as a file entry, and skips one whose key
// requires already holds.
func (a *app) adoptFiles(cmd *cobra.Command, p *project.Project, rep *build.PullReport, as string) error {
	if len(rep.Adoptable) == 0 {
		return nil
	}
	_, err := a.relockProject(cmd, p, true, func(_ *project.Project, r *resolve.Resolver) (string, error) {
		for _, rel := range rep.Adoptable {
			abs := filepath.Join(rep.Dir, filepath.FromSlash(rel))
			dest, _, err := r.ProjectPath(abs)
			if err != nil {
				return "", err
			}
			err = r.Add(cmd.Context(), abs, resolve.AddOptions{Type: build.AdoptedType(rel), As: as})
			var taken *out.Error
			if errors.As(err, &taken) && taken.Code == "requires-taken" {
				skip := taken.Message
				if taken.Help != "" {
					skip += "; " + taken.Help
				}
				rep.Skipped = append(rep.Skipped, rel+" ("+skip+")")
				continue
			}
			if err != nil {
				return "", err
			}
			rep.Entries = append(rep.Entries, rel+" -> "+dest)
		}
		return "", nil
	})
	return err
}

func (a *app) pullSource(b *build.Builder, p *project.Project, lf *local.File, side string, files []string) (string, error) {
	buildDir, dirs, err := a.buildDirs(p, lf, side)
	if err != nil || len(dirs) == 0 {
		return "", err
	}
	if len(dirs) == 1 {
		return dirs[0], nil
	}
	var drifted []string
	for _, dir := range dirs {
		rep, err := b.Diff(side, build.Options{Dir: dir, Features: lf.Features})
		if err != nil {
			if dir == buildDir {
				return "", err
			}
			a.printer.Warn("skipped %s: %v", dir, err)
			continue
		}
		if driftsNamed(rep, files) {
			drifted = append(drifted, dir)
		}
	}
	switch len(drifted) {
	case 0:
		return dirs[0], nil
	case 1:
		return drifted[0], nil
	}
	e := out.Errorf("ambiguous-into", "the %s side has edits in several directories", side)
	e.Help = "pass --into"
	e.Candidates, e.Flag = drifted, "--into"
	return "", e
}

func driftsNamed(rep *build.DiffReport, files []string) bool {
	if len(files) == 0 {
		return len(rep.Files) > 0
	}
	for _, f := range files {
		rel := filepath.ToSlash(filepath.Clean(f))
		for _, d := range rep.Files {
			if d.Path == rel {
				return true
			}
		}
	}
	return false
}

// diffAside says what happened to a file in words; JSON keeps the state name.
func diffAside(state string) string {
	switch state {
	case "kept":
		return "edited here"
	case "conflict":
		return "edited on both sides"
	case "untracked":
		return "not from shulker"
	case "orphan":
		return "no longer in overrides"
	}
	return state
}

func diffKind(state string) out.Kind {
	switch state {
	case "write":
		return out.Add
	case "remove", "orphan":
		return out.Drop
	}
	return out.Change
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
