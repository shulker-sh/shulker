package cli

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cfpack"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
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
	Sides     []string          `json:"sides"`
	Mods      *resolve.Imported `json:"mods"`
	Overrides []string          `json:"overrides"`
}

func (a *app) importCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Create a project from another modpack format",
	}
	cmd.AddCommand(a.importMrpackCmd(), a.importCurseForgeCmd())
	return cmd
}

func (a *app) importMrpackCmd() *cobra.Command {
	var name string
	var ignoreShulker bool
	cmd := &cobra.Command{
		Use:         "mrpack <file>",
		Annotations: acts(),
		Short:       "Create a project from a Modrinth modpack (.mrpack)",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arc, err := mrpack.Read(args[0])
			if err != nil {
				return err
			}
			if ignoreShulker {
				arc.Marker = nil
			}
			if name == "" {
				name = slugify(arc.Index.Name)
			}
			dir, err := a.importDir(name)
			if err != nil {
				return err
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
			if exact.Minecraft == "" {
				return out.Errorf("mrpack-invalid", "the modpack's index names no minecraft version")
			}
			_, exact.Loader.Version, _ = arc.Loader()
			l, err := a.importLock(cmd.Context(), d, &exact)
			if err != nil {
				return err
			}
			if arc.Marker != nil && arc.Marker.Manifest.Server != nil && arc.Marker.Manifest.Server.Players != nil {
				l.Players = arc.Marker.Lock.Players
			}
			r := &resolve.Resolver{Dir: dir, Manifest: m, Lock: l, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
			mods, err := r.ImportMrpack(cmd.Context(), arc)
			if err != nil {
				return err
			}
			a.warn(mods.Warnings)
			if err := r.AdoptLocalFiles(); err != nil {
				return err
			}
			if arc.Marker != nil {
				mods.Overrides = dropManifestOwned(m, mods.Overrides)
			}
			if err := writeImportIcon(dir, m, arc.Icon); err != nil {
				return err
			}
			if err := writeImport(dir, m, l, mods.Overrides); err != nil {
				return err
			}
			res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: l.Minecraft, Loader: l.Loader, Marker: arc.Marker != nil, Sides: m.Sides(), Mods: mods, Overrides: overridePaths(mods.Overrides)}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OKInto("imported "+res.Name+" "+res.Version, dir, platformLabel(res.Minecraft, res.Loader.Type, res.Loader.Version))
				rows := []out.Row{
					{Text: fmt.Sprintf("%s, %d reused from the shulker marker", lockedSummary(mods.Locked), len(mods.Reused))},
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
	cmd.Flags().BoolVar(&ignoreShulker, "ignore-shulker", false, "ignore the shulker manifest and lock inside the modpack and import it as any other one")
	return cmd
}

func (a *app) importCurseForgeCmd() *cobra.Command {
	var name string
	var ignoreShulker bool
	cmd := &cobra.Command{
		Use:         "curseforge <file>",
		Annotations: acts(),
		Short:       "Create a project from a CurseForge modpack (.zip)",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arc, err := cfpack.Read(args[0])
			if err != nil {
				return err
			}
			if ignoreShulker {
				arc.Marker = nil
			}
			if name == "" {
				name = slugify(arc.Manifest.Name)
			}
			dir, err := a.importDir(name)
			if err != nil {
				return err
			}
			asMrpack, err := arc.Mrpack()
			if err != nil {
				return err
			}
			asMrpack.Marker = arc.Marker
			m, warnings, err := importManifest(asMrpack, name)
			if err != nil {
				return err
			}
			a.warn(warnings)
			if arc.Marker == nil && arc.Manifest.Author != "" {
				m.Authors = []string{arc.Manifest.Author}
			}
			d, err := a.deps()
			if err != nil {
				return err
			}
			exact := *m
			exact.Minecraft = arc.Manifest.Minecraft.Version
			exact.Loader.Type, exact.Loader.Version, _ = arc.Loader()
			l, err := a.importLock(cmd.Context(), d, &exact)
			if err != nil {
				return err
			}
			if arc.Marker != nil && arc.Marker.Manifest.Server != nil && arc.Marker.Manifest.Server.Players != nil {
				l.Players = arc.Marker.Lock.Players
			}
			r := &resolve.Resolver{Dir: dir, Manifest: m, Lock: l, Providers: d.providers, Cache: d.cache, Fetch: d.fetch, Log: a.progress}
			mods, err := r.ImportCurseForge(cmd.Context(), arc)
			if err != nil {
				return err
			}
			a.warn(mods.Warnings)
			if err := r.AdoptLocalFiles(); err != nil {
				return err
			}
			if arc.Marker != nil {
				mods.Overrides = dropManifestOwned(m, mods.Overrides)
			}
			if err := writeImportIcon(dir, m, arc.Icon); err != nil {
				return err
			}
			if err := writeImport(dir, m, l, mods.Overrides); err != nil {
				return err
			}
			res := importResult{Dir: dir, Name: m.Name, Version: m.Version, Minecraft: l.Minecraft, Loader: l.Loader, Marker: arc.Marker != nil, Sides: m.Sides(), Mods: mods, Overrides: overridePaths(mods.Overrides)}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.OKInto("imported "+res.Name+" "+res.Version, dir, platformLabel(res.Minecraft, res.Loader.Type, res.Loader.Version))
				locked := lockedSummary(mods.Locked)
				if res.Marker {
					locked += fmt.Sprintf(", %d reused from the shulker marker", len(mods.Reused))
				}
				rows := []out.Row{
					{Text: locked},
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
	cmd.Flags().BoolVar(&ignoreShulker, "ignore-shulker", false, "ignore the shulker manifest and lock inside the modpack and import it as any other one")
	return cmd
}

// lockedSummary counts an import's locked files by type, naming each type's providers when the
// files came from more than one, or the one provider after them all when they didn't.
func lockedSummary(files []resolve.LockedFile) string {
	if len(files) == 0 {
		return plural(0, "file", "files") + " locked"
	}
	byType := map[string]map[string]int{}
	providers := map[string]bool{}
	for _, f := range files {
		if byType[f.Type] == nil {
			byType[f.Type] = map[string]int{}
		}
		byType[f.Type][f.Provider]++
		providers[f.Provider] = true
	}
	var parts []string
	for _, kind := range []string{manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack} {
		counts := byType[kind]
		if counts == nil {
			continue
		}
		names := slices.Collect(maps.Keys(counts))
		slices.SortFunc(names, func(a, b string) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
		})
		total := 0
		var from []string
		for _, name := range names {
			total += counts[name]
			if len(names) == 1 {
				from = append(from, provider.Title(name))
			} else {
				from = append(from, fmt.Sprintf("%d %s", counts[name], provider.Title(name)))
			}
		}
		one, many := typeNouns(kind)
		part := plural(total, one, many)
		if len(providers) > 1 {
			part += " (" + strings.Join(from, ", ") + ")"
		}
		parts = append(parts, part)
	}
	summary := strings.Join(parts, ", ") + " locked"
	if len(providers) == 1 {
		summary += " from " + provider.Title(files[0].Provider)
	}
	return summary
}

func typeNouns(kind string) (one, many string) {
	switch kind {
	case manifest.TypeResourcePack:
		return "resource pack", "resource packs"
	case manifest.TypeShader:
		return "shader", "shaders"
	case manifest.TypeDatapack:
		return "datapack", "datapacks"
	}
	return "mod", "mods"
}

// importDir is where an import creates its project: --dir, else a folder named for the project.
// It refuses one that already holds a manifest.
func (a *app) importDir(name string) (string, error) {
	dir := a.dir
	if dir == "" {
		dir = name
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return "", out.Errorf("manifest-exists", "%s already exists in %s", manifest.FileName, dir)
	}
	return dir, nil
}

// importLock starts the new project's lock from the exact platform the pack names.
func (a *app) importLock(ctx context.Context, d *deps, exact *manifest.Manifest) (*lock.Lock, error) {
	a.progress("%s", resolvingLine(exact.Minecraft, exact.Loader))
	platform, err := d.meta.Platform(ctx, exact, nil)
	if err != nil {
		return nil, err
	}
	l := lock.New()
	l.Minecraft = platform.Minecraft
	l.Loader = platform.Loader
	l.Java = platform.Java
	return l, nil
}

func importManifest(arc *mrpack.Archive, name string) (*manifest.Manifest, []string, error) {
	minecraft := arc.Index.Dependencies["minecraft"]
	if minecraft == "" {
		return nil, nil, out.Errorf("mrpack-invalid", "the index has no minecraft dependency")
	}
	loaderType, loaderVersion, _ := arc.Loader()
	var warnings []string
	if arc.Marker == nil {
		m := &manifest.Manifest{
			Schema:    manifest.SchemaURL,
			Name:      name,
			Version:   arc.Index.VersionID,
			Note:      arc.Index.Summary,
			Minecraft: minecraft,
			Loader:    manifest.Loader{Type: loaderType, Version: loaderVersion},
			Requires:  map[string]manifest.Require{},
			Client:    &manifest.Client{},
		}
		if importNeedsServer(arc) {
			m.Server = &manifest.Server{Memory: server.DefaultMemory}
		}
		return m, warnings, nil
	}
	copied := *arc.Marker.Manifest
	m := &copied
	ml := arc.Marker.Lock
	if ml.Minecraft != minecraft {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to Minecraft %s but the pack is for %s; using %s", ml.Minecraft, minecraft, minecraft))
		m.Minecraft = minecraft
	}
	if ml.Loader.Type != loaderType || ml.Loader.Version != loaderVersion {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to %s but the pack is for %s; using the pack's", loader.Describe(ml.Loader.Type, ml.Loader.Version), loader.Describe(loaderType, loaderVersion)))
		m.Loader = manifest.Loader{Type: loaderType, Version: loaderVersion}
	}
	if m.Version != arc.Index.VersionID {
		m.Version = arc.Index.VersionID
	}
	if modpacks := m.Modpacks(); len(modpacks) > 0 {
		sources := make([]string, 0, len(modpacks))
		for _, name := range slices.Sorted(maps.Keys(modpacks)) {
			source := modpacks[name].Source + modpacks[name].File
			if source == "" {
				source = name
			}
			sources = append(sources, source)
		}
		warnings = append(warnings, fmt.Sprintf("pack layers were flattened into the overrides: %s", strings.Join(sources, ", ")))
	}
	m.Requires = map[string]manifest.Require{}
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
		owned[m.OptionsPath()] = true
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

// writeImportIcon puts the archive's icon back where the restored manifest names it, and drops
// the key from an archive that carries none, so the project stays valid.
func writeImportIcon(dir string, m *manifest.Manifest, icon []byte) error {
	if m.Icon == "" {
		return nil
	}
	if icon == nil {
		m.Icon = ""
		return nil
	}
	path := filepath.Join(dir, filepath.FromSlash(m.Icon))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.Write(path, icon)
}

func overridePaths(overrides []mrpack.Override) []string {
	paths := make([]string, 0, len(overrides))
	for _, o := range overrides {
		paths = append(paths, o.Layer+"/"+o.Path)
	}
	sort.Strings(paths)
	return paths
}
