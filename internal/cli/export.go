package cli

import (
	"context"
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
	"shulker.sh/shulker/internal/provider/curseforge"
)

func (a *app) exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write this project in another modpack format",
	}
	cmd.AddCommand(a.exportMrpackCmd(), a.exportCurseForgeCmd())
	return cmd
}

type exportFlags struct {
	version, output, target, osName, ref string
	bundle                               bool
	ff                                   featureFlags
}

func (f *exportFlags) register(cmd *cobra.Command, extension, target, bundle string) {
	cmd.Flags().StringVar(&f.version, "version", "", "version written into the pack (default: \"version\" in shulker.json)")
	cmd.Flags().StringVarP(&f.output, "output", "o", "", "archive path (default: build/<name>-<version>"+extension+", or the current directory for a git or URL source)")
	cmd.Flags().StringVar(&f.target, "target", "", target)
	cmd.Flags().StringVar(&f.osName, "os", "", "include mods gated on this os: macos, windows, or linux (default: leave them out)")
	cmd.Flags().BoolVar(&f.bundle, "bundle", false, bundle)
	cmd.Flags().StringVar(&f.ref, "ref", "", "branch, tag, or commit to export from a git source (default: the remote HEAD)")
	f.ff.register(cmd, "for this run only")
}

type exportJob struct {
	project  *project.Project
	builder  *build.Builder
	version  string
	output   string
	features map[string]bool
}

func (a *app) openExport(ctx context.Context, args []string, f *exportFlags, fileName func(*manifest.Manifest, string) string) (*exportJob, error) {
	if err := checkOS(f.osName); err != nil {
		return nil, err
	}
	src, err := a.linkSource(ctx, args, f.ref)
	if err != nil {
		return nil, err
	}
	p := src.project
	if diffs := p.LockDifferences(); len(diffs) > 0 && !src.remote() {
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
		dir := filepath.Join(p.Dir, "build")
		if src.remote() {
			if dir = a.dir; dir == "" {
				if dir, err = os.Getwd(); err != nil {
					return nil, err
				}
			}
		}
		job.output = filepath.Join(dir, fileName(p.Manifest, job.version))
	}
	if job.output, err = filepath.Abs(job.output); err != nil {
		return nil, err
	}
	if src.remote() {
		if _, err := a.fetchLocked(ctx, p, false); err != nil {
			return nil, err
		}
	}
	if job.builder, err = a.builder(ctx, p); err != nil {
		return nil, err
	}
	lf, err := local.Load(p.Dir)
	if err != nil {
		return nil, err
	}
	if job.features, err = featureOverrides(job.builder, lf.Features, f.ff); err != nil {
		return nil, err
	}
	return job, nil
}

// withBundleNudge points the nudge under a bundle error at the command as typed, source included.
func (a *app) withBundleNudge(err error) error {
	var e *out.Error
	if errors.As(err, &e) && e.Nudge.Command != "" && (e.Code == "curseforge-not-found" || e.Code == "mrpack-host-not-allowed") {
		e.Nudge.Command = out.CommandLine(append(slices.Clone(a.printer.Args), "--bundle"))
	}
	return err
}

func (a *app) exportMrpackCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "mrpack [source]",
		Short: "Export a Modrinth modpack (.mrpack) for the Modrinth app and other launchers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := a.openExport(cmd.Context(), args, &f, build.MrpackFileName)
			if err != nil {
				return err
			}
			opts := build.MrpackOptions{VersionID: job.version, Output: job.output, Bundle: f.bundle, OS: f.osName, Features: job.features}
			if f.target != "" {
				opts.Targets = []string{f.target}
			}
			rep, err := job.builder.ExportMrpack(opts)
			if err != nil {
				return a.withBundleNudge(err)
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("wrote "+rep.Name+" "+rep.VersionID, rep.Path, fmt.Sprintf("%s by download, %d bundled, %s", plural(len(rep.Mods), "mod", "mods"), len(rep.Bundled), plural(len(rep.Overrides), "override file", "override files")))
			})
		},
	}
	f.register(cmd, ".mrpack", "export one target only (default: every target)", "put mods that Modrinth launchers cannot download inside the archive")
	return cmd
}

func (a *app) exportCurseForgeCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "curseforge [source]",
		Short: "Export a CurseForge modpack (.zip) for the CurseForge app",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := a.openExport(cmd.Context(), args, &f, build.CurseForgeFileName)
			if err != nil {
				return err
			}
			target, err := sideTarget(job.project.Manifest, f.target, "client", "export")
			if err != nil {
				return err
			}
			rep, err := job.builder.ExportCurseForge(build.CurseForgeOptions{
				Target:   target,
				Version:  job.version,
				Output:   job.output,
				Bundle:   f.bundle,
				OS:       f.osName,
				Features: job.features,
				Match: func(fingerprints []uint32) (map[uint32]curseforge.Match, error) {
					return a.matchFingerprints(cmd.Context(), fingerprints)
				},
			})
			if err != nil {
				return a.withBundleNudge(err)
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("wrote "+rep.Name+" "+rep.Version, rep.Path, fmt.Sprintf("%s by file ID, %d bundled, %s", plural(len(rep.Mods), "mod", "mods"), len(rep.Bundled), plural(len(rep.Overrides), "override file", "override files")))
				if len(rep.Matched) > 0 {
					l.Tree(out.Row{Label: "matched on CurseForge", Text: strings.Join(rep.Matched, ", ")})
				}
			})
		},
	}
	f.register(cmd, ".zip", "client target to export (default: the only client target)", "put mods that aren't on CurseForge inside the archive")
	return cmd
}

func (a *app) matchFingerprints(ctx context.Context, fingerprints []uint32) (map[uint32]curseforge.Match, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	cf, ok := d.providers["curseforge"].(*curseforge.CurseForge)
	if !ok {
		e := out.Errorf("provider-unavailable", "curseforge needs an API key to look mods up")
		e.Help = "set " + curseforge.KeyEnv + " or run `shulker config set curseforge.key <key>`"
		return nil, e
	}
	a.progress("looking up %s on CurseForge", plural(len(fingerprints), "mod", "mods"))
	return cf.MatchFingerprints(ctx, fingerprints)
}
