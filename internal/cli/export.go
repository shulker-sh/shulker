package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
)

func (a *app) exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write this project in another modpack format",
	}
	cmd.AddCommand(a.exportMrpackCmd())
	return cmd
}

func (a *app) exportMrpackCmd() *cobra.Command {
	var version, output, target, osName string
	var bundle bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "mrpack",
		Short: "Export a Modrinth modpack (.mrpack) for the Modrinth app and other launchers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(osName); err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if diffs := p.LockDifferences(); len(diffs) > 0 {
				e := out.Errorf("lock-stale", "shulker.lock does not match shulker.json (%s)", strings.Join(diffs, "; "))
				e.Help = "run `shulker lock`"
				e.Items = diffs
				return e
			}
			if version == "" {
				version = p.Manifest.Version
			}
			if version == "" {
				return out.Errorf("version-required", "set \"version\" in shulker.json or pass --version")
			}
			if output == "" {
				output = filepath.Join(p.Dir, "build", build.MrpackFileName(p.Manifest, version))
			}
			if output, err = filepath.Abs(output); err != nil {
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
			overrides, err := featureOverrides(b, lf.Features, ff)
			if err != nil {
				return err
			}
			opts := build.MrpackOptions{VersionID: version, Output: output, Bundle: bundle, OS: osName, Features: overrides}
			if target != "" {
				opts.Targets = []string{target}
			}
			rep, err := b.ExportMrpack(opts)
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("wrote "+rep.Name+" "+rep.VersionID, output, fmt.Sprintf("%s by download, %d bundled, %s", plural(len(rep.Mods), "mod", "mods"), len(rep.Bundled), plural(len(rep.Overrides), "override file", "override files")))
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "version id written into the pack (default: \"version\" in shulker.json)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "archive path (default: build/<name>-<version>.mrpack)")
	cmd.Flags().StringVar(&target, "target", "", "export one target only (default: every target)")
	cmd.Flags().StringVar(&osName, "os", "", "include mods gated on this os: macos, windows, or linux (default: leave them out)")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "put mods that Modrinth launchers cannot download inside the archive")
	ff.register(cmd, "for this run only")
	return cmd
}
