package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
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
		loaderName    string
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
				return out.Errorf("manifest-exists", "%s already exists here", manifest.FileName)
			}
			if !yes && (minecraft == "" || name == "") {
				return out.Errorf("usage", "pass --yes for defaults or set --name and --minecraft; interactive prompts are not implemented yet")
			}
			if target != "client" && target != "server" {
				e := out.Errorf("usage", "--target must be client or server, not %q", target)
				e.Candidates, e.Given, e.Flag = []string{"client", "server"}, target, "--target"
				return e
			}
			if _, ok := loader.Lookup(loaderName); !ok {
				e := out.Errorf("usage", "unknown loader %q; use one of %s", loaderName, strings.Join(loader.Names(), ", "))
				e.Candidates, e.Given, e.Flag = loader.Names(), loaderName, "--loader"
				return e
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
				Authors:   defaultAuthors(),
				Minecraft: minecraft,
				Loader:    manifest.Loader{Type: loaderName, Version: loaderVersion},
				Targets:   map[string]manifest.Target{target: {Side: target, Overrides: []string{"overrides"}, Build: "build/" + target}},
				Mods:      map[string]manifest.Mod{},
			}
			if target == "server" {
				m.Server = &manifest.Server{Eula: false, Memory: server.DefaultMemory, Properties: map[string]any{"difficulty": "easy"}}
			}
			if target == "client" {
				m.Client = &manifest.Client{Options: map[string]any{
					"onboardAccessibility":   false,
					"skipMultiplayerWarning": true,
					"tutorialStep":           "none",
					"joinedFirstServer":      true,
				}}
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			a.progress("resolving Minecraft %s with %s %s", minecraft, loaderName, loaderVersion)
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
			if err := p.SaveManifest(); err != nil {
				return err
			}
			if err := p.SaveLock(); err != nil {
				os.Remove(filepath.Join(dir, manifest.FileName))
				return err
			}
			if err := scaffold(dir); err != nil {
				return err
			}
			res := initResult{Name: name, Minecraft: l.Minecraft, Loader: l.Loader.Type, Version: l.Loader.Version, Java: l.Java.Major, Target: target}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OK("created "+manifest.FileName, fmt.Sprintf("Minecraft %s, %s %s, Java %d", res.Minecraft, res.Loader, res.Version, res.Java))
				l.Nudge("Add a mod", "shulker add <mod>")
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept defaults: latest release, fabric, client target")
	cmd.Flags().StringVar(&name, "name", "", "project name (default: directory name)")
	cmd.Flags().StringVar(&minecraft, "minecraft", "", "Minecraft version or range (default: latest release)")
	cmd.Flags().StringVar(&loaderName, "loader", "fabric", "mod loader: "+strings.Join(loader.Names(), ", "))
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
		return os.WriteFile(gi, []byte("/build/\n/data/\n/downloads/\n/shulker.local.json\n"), 0o644)
	}
	return nil
}

func defaultAuthors() []string {
	authors := []string{"shulker.sh"}
	name, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return authors
	}
	if user := strings.TrimSpace(string(name)); user != "" {
		authors = append([]string{user}, authors...)
	}
	return authors
}
