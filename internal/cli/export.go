package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
)

func (a *app) exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write this project in another modpack format",
	}
	for _, f := range packarchive.Formats {
		cmd.AddCommand(a.exportFormatCmd(f))
	}
	return cmd
}

type exportFlags struct {
	version, output, side, osName string
	at                            modpack.At
	bundle, assumeClient          bool
	ff                            featureFlags
}

func (e *exportFlags) register(cmd *cobra.Command, extension, bundle string) {
	cmd.Flags().StringVar(&e.version, "version", "", "version written into the pack (default: \"version\" in shulker.json)")
	cmd.Flags().StringVarP(&e.output, "output", "o", "", "archive path (default: build/<name>-<version>"+extension+", or the current directory for a git or URL source)")
	cmd.Flags().StringVar(&e.osName, "os", "", "include mods gated on this os: macos, windows, or linux (default: leave them out).")
	cmd.Flags().BoolVar(&e.bundle, "bundle", false, bundle)
	cmd.Flags().BoolVar(&e.assumeClient, "assume-client", false, "export a client even when the source declares none, built from the mods and overrides both sides share.")
	cmd.Flags().StringVar(&e.at.Ref, "ref", "", "branch, tag, or commit to export from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&e.at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	e.ff.register(cmd, "for this run only")
}

type exportJob struct {
	project  *project.Project
	builder  *build.Builder
	version  string
	output   string
	sides    []string
	features map[string]bool
}

func (a *app) openExport(ctx context.Context, args []string, f *exportFlags, format packarchive.Format, sides func(*manifest.Manifest) ([]string, error)) (*exportJob, error) {
	if err := checkOS(f.osName); err != nil {
		return nil, err
	}
	src, err := a.linkSource(ctx, args, f.at)
	if err != nil {
		return nil, err
	}
	p := src.Project
	if diffs := p.LockDifferences(); len(diffs) > 0 && !src.IsRemote() {
		e := out.Errorf("lock-stale", "shulker.lock does not match shulker.json (%s)", strings.Join(diffs, "; "))
		e.Help = "run `shulker lock`"
		e.Items = diffs
		return nil, e
	}
	job := &exportJob{project: p, version: f.version, output: f.output}
	if job.version == "" {
		job.version = p.Manifest.Version
	}
	if job.version == "" {
		return nil, out.Errorf("version-required", "set \"version\" in shulker.json or pass --version")
	}
	if job.output == "" {
		cwd := a.dir
		if src.IsRemote() && cwd == "" {
			if cwd, err = os.Getwd(); err != nil {
				return nil, err
			}
		}
		job.output = build.ExportPath(p.Dir, p.Manifest, job.version, format, src.IsRemote(), cwd)
	}
	if job.output, err = filepath.Abs(job.output); err != nil {
		return nil, err
	}
	if job.sides, err = sides(p.Manifest); err != nil {
		return nil, err
	}
	if _, err := a.fetchLocked(ctx, p, job.sides, false); err != nil {
		return nil, err
	}
	if job.builder, err = a.builder(ctx, p); err != nil {
		return nil, err
	}
	lf, err := a.loadLocal(p.Dir)
	if err != nil {
		return nil, err
	}
	if job.features, err = f.ff.overrides(job.builder, lf.Features); err != nil {
		return nil, err
	}
	return job, nil
}

// withBundleNudge points the nudge under a bundle error at the command as typed, source included.
func (a *app) withBundleNudge(err error) error {
	var e *out.Error
	if errors.As(err, &e) && strings.HasSuffix(e.Nudge.Command, " --bundle") {
		e.Nudge.Command = out.CommandLine(append(slices.Clone(a.printer.Args), "--bundle"))
	}
	return err
}

// exportTally gives each kind its own row, so a resource pack is never counted
// as a mod. A row with nothing in it is left out.
type exportTally struct {
	how                  string
	mods                 []string
	resourcePacks        []string
	shaders              []string
	datapacks            []string
	bundledMods          []string
	bundledResourcePacks []string
	bundledShaders       []string
	bundledDatapacks     []string
	overrides            []string
}

func (e exportTally) rows() []out.Row {
	var rows []out.Row
	add := func(items []string, one, many, how string) {
		if len(items) == 0 {
			return
		}
		text := out.Count(len(items), one, many)
		if how != "" {
			text += " " + how
		}
		rows = append(rows, out.Row{Text: text})
	}
	add(e.mods, "mod", "mods", e.how)
	add(e.resourcePacks, "resource pack", "resource packs", e.how)
	add(e.shaders, "shader", "shaders", e.how)
	add(e.datapacks, "datapack", "datapacks", e.how)
	add(e.bundledMods, "mod", "mods", "bundled")
	add(e.bundledResourcePacks, "resource pack", "resource packs", "bundled")
	add(e.bundledShaders, "shader", "shaders", "bundled")
	add(e.bundledDatapacks, "datapack", "datapacks", "bundled")
	add(e.overrides, "override file", "override files", "")
	return rows
}

// exportFormatCmd is the export subcommand for one format: the shared flags, plus --side for a
// format that keeps sides apart.
func (a *app) exportFormatCmd(f packarchive.Format) *cobra.Command {
	var flags exportFlags
	usage := f.Usage()
	cmd := &cobra.Command{
		Use:         f.Name() + " [source]",
		Annotations: acts(),
		Short:       usage.Short,
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := a.openExport(cmd.Context(), args, &flags, f, func(m *manifest.Manifest) ([]string, error) {
				if f.Sided() {
					sides, assumed, err := project.ExportSides(m, flags.side, flags.assumeClient)
					a.warnAssumedClient(assumed)
					return sides, err
				}
				side, assumed, err := project.ClientSide(m, flags.assumeClient)
				a.warnAssumedClient(assumed)
				return []string{side}, err
			})
			if err != nil {
				return err
			}
			rep, err := job.builder.Export(cmd.Context(), build.ExportOptions{Format: f, Sides: job.sides, Version: job.version, Output: job.output, Bundle: flags.bundle, OS: flags.osName, Features: job.features})
			if err != nil {
				return a.withBundleNudge(err)
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				rows := exportTally{how: usage.Listed, mods: rep.Mods, resourcePacks: rep.ResourcePacks, shaders: rep.Shaders, datapacks: rep.Datapacks, bundledMods: rep.BundledMods, bundledResourcePacks: rep.BundledResourcePacks, bundledShaders: rep.BundledShaders, bundledDatapacks: rep.BundledDatapacks, overrides: rep.Overrides}.rows()
				if len(rep.Matched) > 0 {
					rows = append(rows, out.Row{Label: "matched on " + f.Title(), Text: strings.Join(rep.Matched, ", ")})
				}
				l.OKInto("Wrote "+rep.Name+" "+rep.Version, rep.Path, strings.Join(rep.Sides, " and "), rows...)
			})
		},
	}
	a.scopeFlags(cmd)
	flags.register(cmd, f.Extension(), usage.Bundle)
	a.registerFailFast(cmd)
	if f.Sided() {
		cmd.Flags().StringVar(&flags.side, "side", "", "export one side only (default: every declared side).")
	}
	return cmd
}
