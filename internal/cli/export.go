package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/spf13/cobra"
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
	var version, output, target string
	var bundle bool
	cmd := &cobra.Command{
		Use:   "mrpack",
		Short: "Export a Modrinth modpack (.mrpack) for the Modrinth app and other launchers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale {
				return out.Errorf("lock-stale", "shulker.lock does not match shulker.json; run `shulker add`, `remove`, or `update`")
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
			opts := build.MrpackOptions{VersionID: version, Output: output, Bundle: bundle}
			if target != "" {
				opts.Targets = []string{target}
			}
			rep, err := b.ExportMrpack(opts)
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(w io.Writer) {
				fmt.Fprintf(w, "wrote %s: %s %s, %d mod(s) by download, %d bundled, %d override file(s)\n", output, rep.Name, rep.VersionID, len(rep.Mods), len(rep.Bundled), len(rep.Overrides))
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "version id written into the pack (default: \"version\" in shulker.json)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "archive path (default: build/<name>-<version>.mrpack)")
	cmd.Flags().StringVar(&target, "target", "", "export one target only (default: every target)")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "put mods that Modrinth launchers cannot download inside the archive")
	return cmd
}
