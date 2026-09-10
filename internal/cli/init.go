package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrewmast/shulker/internal/lock"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/andrewmast/shulker/internal/server"
	"github.com/spf13/cobra"
)

type initResult struct {
	Name      string `json:"name"`
	Minecraft string `json:"minecraft"`
	Loader    string `json:"loader"`
	Version   string `json:"loaderVersion"`
	Java      int    `json:"java"`
	Target    string `json:"target"`
}

func (a *app) initCmd() *cobra.Command {
	var (
		yes           bool
		name          string
		minecraft     string
		loader        string
		loaderVersion string
		target        string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create shulker.json and a lock in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := a.dir
			if dir == "" {
				var err error
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}
			if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
				return out.Errorf("exists", "%s already exists here", manifest.FileName)
			}
			if !yes && (minecraft == "" || name == "") {
				return out.Errorf("usage", "pass --yes for defaults or set --name and --minecraft; interactive prompts are not implemented yet")
			}
			if name == "" {
				name = slugify(filepath.Base(dir))
			}
			if minecraft == "" {
				minecraft = "*"
			}
			m := &manifest.Manifest{
				Schema:    manifest.SchemaURL,
				Name:      name,
				Minecraft: minecraft,
				Loader:    manifest.Loader{Type: loader, Version: loaderVersion},
				Targets:   map[string]manifest.Target{target: {Side: target, Overrides: []string{"overrides"}, Build: "build/" + target}},
				Mods:      map[string]manifest.Mod{},
			}
			if target == "server" {
				m.Server = &manifest.Server{Eula: false, Memory: server.DefaultMemory, Properties: map[string]any{"difficulty": "easy"}}
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			a.progress("resolving Minecraft %s with %s %s", minecraft, loader, loaderVersion)
			platform, err := d.meta.Platform(cmd.Context(), m)
			if err != nil {
				return err
			}
			if platform.Minecraft != m.Minecraft && m.Minecraft == "*" {
				m.Minecraft = platform.Minecraft
			}
			l := lock.New()
			l.Minecraft = platform.Minecraft
			l.Loader = platform.Loader
			l.Java = platform.Java
			p := &project.Project{Dir: dir, Manifest: m, Lock: l}
			if err := scaffold(dir); err != nil {
				return err
			}
			if err := p.SaveManifest(); err != nil {
				return err
			}
			if err := p.SaveLock(); err != nil {
				return err
			}
			res := initResult{Name: name, Minecraft: l.Minecraft, Loader: l.Loader.Type, Version: l.Loader.Version, Java: l.Java.Major, Target: target}
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintf(w, "Created %s for Minecraft %s with %s %s (Java %d)\nNext: shulker add <mod>\n", manifest.FileName, res.Minecraft, res.Loader, res.Version, res.Java)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept defaults: latest release, fabric, client target")
	cmd.Flags().StringVar(&name, "name", "", "project name (default: directory name)")
	cmd.Flags().StringVar(&minecraft, "minecraft", "", "Minecraft version or range (default: latest release)")
	cmd.Flags().StringVar(&loader, "loader", "fabric", "mod loader: fabric, quilt, neoforge, forge")
	cmd.Flags().StringVar(&loaderVersion, "loader-version", "*", "loader version range")
	cmd.Flags().StringVar(&target, "target", "client", "first target: client or server")
	return cmd
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "shulker-project"
	}
	return out
}

func scaffold(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "overrides"), 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(gi, []byte("/build/\n/data/\n"), 0o644)
	}
	return nil
}
