package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/server"
)

type importResult struct {
	Dir       string            `json:"dir"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Minecraft string            `json:"minecraft"`
	Loader    lock.Loader       `json:"loader"`
	Marker    bool              `json:"marker"`
	Targets   []string          `json:"targets"`
	Mods      *resolve.Imported `json:"mods"`
	Overrides []string          `json:"overrides"`
}

func (a *app) importCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Create a project from another modpack format",
	}
	cmd.AddCommand(a.importMrpackCmd())
	return cmd
}

func (a *app) importMrpackCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "mrpack <file>",
		Short: "Create a project from a Modrinth modpack (.mrpack)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arc, err := mrpack.Read(args[0])
			if err != nil {
				return err
			}
			if name == "" {
				name = slugify(arc.Index.Name)
			}
			dir := a.dir
			if dir == "" {
				dir = name
			}
			if dir, err = filepath.Abs(dir); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
				return out.Errorf("manifest-exists", "%s already exists in %s", manifest.FileName, dir)
			}
			m, warnings, err := importManifest(arc, name)
			if err != nil {
				return err
			}
			a.warn(warnings)
			d, err := a.deps()
			if err != nil {
				return err
			}
			exact := *m
			exact.Minecraft = arc.Index.Dependencies["minecraft"]
			_, exact.Loader.Version, _ = arc.Loader()
			a.progress("resolving Minecraft %s with %s %s", exact.Minecraft, exact.Loader.Type, exact.Loader.Version)
			platform, err := d.meta.Platform(cmd.Context(), &exact)
			if err != nil {
				return err
			}
			l := lock.New()
			l.Minecraft = platform.Minecraft
			l.Loader = platform.Loader
			l.Java = platform.Java
			if arc.Marker != nil && arc.Marker.Manifest.Server != nil && arc.Marker.Manifest.Server.Players != nil {
				l.Players = arc.Marker.Lock.Players
			}
			r := &resolve.Resolver{Dir: dir, Manifest: m, Lock: l, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
			mods, err := r.ImportMrpack(cmd.Context(), arc)
			if err != nil {
				return err
			}
			a.warn(mods.Warnings)
			if arc.Marker != nil {
				mods.Overrides = dropManifestOwned(m, mods.Overrides)
			}
			setTargetOverrides(m, mods.Overrides)
			if err := writeImport(dir, m, l, mods.Overrides); err != nil {
				return err
			}
			res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: l.Minecraft, Loader: l.Loader, Marker: arc.Marker != nil, Targets: targetNames(m.Targets), Mods: mods, Overrides: overridePaths(mods.Overrides)}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OKInto("imported "+res.Name+" "+res.Version, dir, fmt.Sprintf("Minecraft %s, %s %s", res.Minecraft, res.Loader.Type, res.Loader.Version))
				rows := []out.Row{
					{Text: fmt.Sprintf("%s locked from Modrinth, %d reused from the shulker marker", plural(len(mods.Locked), "mod", "mods"), len(mods.Reused))},
					{Text: fmt.Sprintf("%s, %s", plural(len(mods.Unmanaged), "unmanaged file", "unmanaged files"), plural(len(res.Overrides), "override file", "override files"))},
				}
				if len(mods.Dropped) > 0 {
					rows = append(rows, out.Row{Label: "dropped from the marker, not in the pack", Text: strings.Join(mods.Dropped, ", ")})
				}
				l.Tree(rows...)
				l.Nudge("Download and build it", "cd "+dir+" && shulker install")
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (default: the pack name, slugified)")
	return cmd
}

func importManifest(arc *mrpack.Archive, name string) (*manifest.Manifest, []string, error) {
	minecraft := arc.Index.Dependencies["minecraft"]
	if minecraft == "" {
		return nil, nil, out.Errorf("mrpack-invalid", "the index has no minecraft dependency")
	}
	loaderType, loaderVersion, ok := arc.Loader()
	if !ok {
		return nil, nil, out.Errorf("mrpack-unsupported", "the index names no known loader (%s)", strings.Join(loader.Names(), ", "))
	}
	var warnings []string
	if arc.Marker == nil {
		m := &manifest.Manifest{
			Schema:    manifest.SchemaURL,
			Name:      name,
			Version:   arc.Index.VersionID,
			Note:      arc.Index.Summary,
			Minecraft: minecraft,
			Loader:    manifest.Loader{Type: loaderType, Version: loaderVersion},
			Targets:   map[string]manifest.Target{"client": {Side: "client", Build: "build/client"}},
			Mods:      map[string]manifest.Mod{},
		}
		if importNeedsServer(arc) {
			m.Targets["server"] = manifest.Target{Side: "server", Build: "build/server"}
			m.Server = &manifest.Server{Memory: server.DefaultMemory}
		}
		return m, warnings, nil
	}
	copied := *arc.Marker.Manifest
	m := &copied
	ml := arc.Marker.Lock
	if ml.Minecraft != minecraft {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to Minecraft %s but the pack targets %s; using %s", ml.Minecraft, minecraft, minecraft))
		m.Minecraft = minecraft
	}
	if ml.Loader.Type != loaderType || ml.Loader.Version != loaderVersion {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to %s %s but the pack targets %s %s; using the pack's", ml.Loader.Type, ml.Loader.Version, loaderType, loaderVersion))
		m.Loader = manifest.Loader{Type: loaderType, Version: loaderVersion}
	}
	if m.Version != arc.Index.VersionID {
		m.Version = arc.Index.VersionID
	}
	if len(m.Packs) > 0 {
		names := make([]string, 0, len(m.Packs))
		for _, p := range m.Packs {
			names = append(names, p.Source)
		}
		warnings = append(warnings, fmt.Sprintf("pack layers were flattened into the overrides: %s", strings.Join(names, ", ")))
		m.Packs = nil
	}
	m.Mods = map[string]manifest.Mod{}
	return m, warnings, nil
}

func importNeedsServer(arc *mrpack.Archive) bool {
	for _, f := range arc.Index.Files {
		if f.Side() == "server" {
			return true
		}
	}
	for _, o := range arc.Overrides {
		if o.Layer == "server-overrides" {
			return true
		}
	}
	return false
}

func dropManifestOwned(m *manifest.Manifest, overrides []mrpack.Override) []mrpack.Override {
	owned := map[string]bool{}
	if m.Client != nil && m.Client.Options != nil {
		owned[build.OptionsFile] = true
	}
	if m.Server != nil {
		owned[build.PropertiesFile] = true
		owned[build.EulaFile] = true
		if m.Server.Players != nil {
			owned[build.WhitelistFile] = true
			owned[build.OpsFile] = true
			owned[build.BansFile] = true
		}
	}
	kept := overrides[:0]
	for _, o := range overrides {
		if !owned[o.Path] {
			kept = append(kept, o)
		}
	}
	return kept
}

func setTargetOverrides(m *manifest.Manifest, overrides []mrpack.Override) {
	present := map[string]bool{}
	for _, o := range overrides {
		present[o.Layer] = true
	}
	for name, t := range m.Targets {
		t.Overrides = []string{"overrides"}
		if layer := t.Side + "-overrides"; present[layer] {
			t.Overrides = append(t.Overrides, layer)
		}
		m.Targets[name] = t
	}
}

func writeImport(dir string, m *manifest.Manifest, l *lock.Lock, overrides []mrpack.Override) error {
	if err := scaffold(dir); err != nil {
		return err
	}
	for _, o := range overrides {
		abs := filepath.Join(dir, o.Layer, filepath.FromSlash(o.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, o.Data, 0o644); err != nil {
			return err
		}
	}
	p := &project.Project{Dir: dir, Manifest: m, Lock: l}
	if err := p.SaveManifest(); err != nil {
		return err
	}
	return p.SaveLock()
}

func overridePaths(overrides []mrpack.Override) []string {
	paths := make([]string, 0, len(overrides))
	for _, o := range overrides {
		paths = append(paths, o.Layer+"/"+o.Path)
	}
	sort.Strings(paths)
	return paths
}
