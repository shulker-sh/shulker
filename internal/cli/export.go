package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
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
	version, output, side, osName, ref string
	bundle, assumeClient               bool
	ff                                 featureFlags
}

func (e *exportFlags) register(cmd *cobra.Command, extension, bundle string) {
	cmd.Flags().StringVar(&e.version, "version", "", "version written into the pack (default: \"version\" in shulker.json)")
	cmd.Flags().StringVarP(&e.output, "output", "o", "", "archive path (default: build/<name>-<version>"+extension+", or the current directory for a git or URL source)")
	cmd.Flags().StringVar(&e.osName, "os", "", "include mods gated on this os: macos, windows, or linux (default: leave them out)")
	cmd.Flags().BoolVar(&e.bundle, "bundle", false, bundle)
	cmd.Flags().BoolVar(&e.assumeClient, "assume-client", false, "export a client even when the source declares none, built from the mods and overrides both sides share")
	cmd.Flags().StringVar(&e.ref, "ref", "", "branch, tag, or commit to export from a git source (default: the remote HEAD)")
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

func (a *app) openExport(ctx context.Context, args []string, f *exportFlags, fileName func(*manifest.Manifest, string) string, sides func(*manifest.Manifest) ([]string, error)) (*exportJob, error) {
	if err := checkOS(f.osName); err != nil {
		return nil, err
	}
	src, err := a.linkSource(ctx, args, f.ref)
	if err != nil {
		return nil, err
	}
	p := src.project
	if diffs := p.LockDifferences(); len(diffs) > 0 && !src.isRemote() {
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
		if src.isRemote() {
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
		text := plural(len(items), one, many)
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

func (a *app) exportMrpackCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:         "mrpack [source]",
		Annotations: acts(),
		Short:       "Export a Modrinth modpack (.mrpack) for the Modrinth app and other launchers",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := a.openExport(cmd.Context(), args, &f, build.MrpackFileName, func(m *manifest.Manifest) ([]string, error) {
				return a.exportSides(m, f.side, f.assumeClient)
			})
			if err != nil {
				return err
			}
			rep, err := job.builder.ExportMrpack(build.MrpackOptions{VersionID: job.version, Output: job.output, Sides: job.sides, Bundle: f.bundle, OS: f.osName, Features: job.features})
			if err != nil {
				return a.withBundleNudge(err)
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("wrote "+rep.Name+" "+rep.VersionID, rep.Path, strings.Join(rep.Sides, " and "))
				l.Tree(exportTally{how: "by download", mods: rep.Mods, resourcePacks: rep.ResourcePacks, shaders: rep.Shaders, datapacks: rep.Datapacks, bundledMods: rep.BundledMods, bundledResourcePacks: rep.BundledResourcePacks, bundledShaders: rep.BundledShaders, bundledDatapacks: rep.BundledDatapacks, overrides: rep.Overrides}.rows()...)
			})
		},
	}
	f.register(cmd, ".mrpack", "put files that Modrinth launchers cannot download inside the archive")
	cmd.Flags().StringVar(&f.side, "side", "", "export one side only (default: every declared side)")
	return cmd
}

func (a *app) exportCurseForgeCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:         "curseforge [source]",
		Annotations: acts(),
		Short:       "Export a CurseForge modpack (.zip) for the CurseForge app",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			job, err := a.openExport(cmd.Context(), args, &f, build.CurseForgeFileName, func(m *manifest.Manifest) ([]string, error) {
				side, err := a.clientSide(m, f.assumeClient)
				return []string{side}, err
			})
			if err != nil {
				return err
			}
			rep, err := job.builder.ExportCurseForge(build.CurseForgeOptions{
				Side:     job.sides[0],
				Version:  job.version,
				Output:   job.output,
				Bundle:   f.bundle,
				OS:       f.osName,
				Features: job.features,
				Match: func(fingerprints []uint32) (map[uint32]curseforge.Match, error) {
					return a.matchFingerprints(cmd.Context(), fingerprints)
				},
				Records: func(projectIDs []int) (map[int]curseforge.Record, error) {
					return a.curseForgeRecords(cmd.Context(), projectIDs)
				},
				Lookalike: func(miss build.CurseForgeMiss) (curseforge.Match, []byte, bool, error) {
					return a.curseForgeLookalike(cmd.Context(), miss)
				},
			})
			if err != nil {
				return a.withBundleNudge(err)
			}
			a.warn(rep.Warnings)
			return a.printer.Emit(rep, func(l *out.Lines) {
				l.OKInto("wrote "+rep.Name+" "+rep.Version, rep.Path, "")
				rows := exportTally{how: "by file ID", mods: rep.Mods, resourcePacks: rep.ResourcePacks, shaders: rep.Shaders, datapacks: rep.Datapacks, bundledMods: rep.BundledMods, bundledResourcePacks: rep.BundledResourcePacks, bundledShaders: rep.BundledShaders, bundledDatapacks: rep.BundledDatapacks, overrides: rep.Overrides}.rows()
				if len(rep.Matched) > 0 {
					rows = append(rows, out.Row{Label: "matched on CurseForge", Text: strings.Join(rep.Matched, ", ")})
				}
				l.Tree(rows...)
			})
		},
	}
	f.register(cmd, ".zip", "put files that aren't on CurseForge inside the archive")
	return cmd
}

func (a *app) matchFingerprints(ctx context.Context, fingerprints []uint32) (map[uint32]curseforge.Match, error) {
	cf, err := a.curseForgeLookup()
	if err != nil {
		return nil, err
	}
	a.progress("looking up %s on CurseForge", plural(len(fingerprints), "file", "files"))
	return cf.MatchFingerprints(ctx, fingerprints)
}

// curseForgeLookalike finds the CurseForge project through the lock's alias or the
// Modrinth slug, and downloads its file with the missed file's name and size.
func (a *app) curseForgeLookalike(ctx context.Context, miss build.CurseForgeMiss) (curseforge.Match, []byte, bool, error) {
	cf, err := a.curseForgeLookup()
	if err != nil {
		return curseforge.Match{}, nil, false, err
	}
	d, err := a.deps()
	if err != nil {
		return curseforge.Match{}, nil, false, err
	}
	projectID := strconv.Itoa(miss.Alias)
	if miss.Alias == 0 {
		if miss.Provider != "modrinth" {
			return curseforge.Match{}, nil, false, nil
		}
		a.progress("looking up %s on CurseForge by slug", miss.Key)
		mr, err := d.providers["modrinth"].Project(ctx, fmt.Sprint(miss.Project), miss.Kind)
		if errors.Is(err, provider.ErrNotFound) {
			return curseforge.Match{}, nil, false, nil
		}
		if err != nil {
			return curseforge.Match{}, nil, false, err
		}
		p, err := cf.Project(ctx, mr.Slug, miss.Kind)
		if errors.Is(err, provider.ErrNotFound) {
			return curseforge.Match{}, nil, false, nil
		}
		if err != nil {
			return curseforge.Match{}, nil, false, err
		}
		projectID = p.ID
	}
	versions, err := cf.Versions(ctx, projectID, miss.Minecraft, miss.Loaders)
	if errors.Is(err, provider.ErrNotFound) || errors.Is(err, fetch.ErrNotFound) {
		return curseforge.Match{}, nil, false, nil
	}
	if err != nil {
		return curseforge.Match{}, nil, false, err
	}
	for _, v := range versions {
		if v.File.Filename != miss.Filename || v.File.Size != miss.Size || v.File.URL == "" {
			continue
		}
		modID, _ := strconv.Atoi(v.ProjectID)
		fileID, _ := strconv.Atoi(v.ID)
		a.progress("comparing %s with CurseForge file %d", miss.Key, fileID)
		var buf bytes.Buffer
		_, err := d.fetch.Download(ctx, v.File.URL, &buf)
		if errors.Is(err, fetch.ErrNotFound) || errors.Is(err, fetch.ErrForbidden) {
			return curseforge.Match{}, nil, false, nil
		}
		if err != nil {
			return curseforge.Match{}, nil, false, err
		}
		return curseforge.Match{ModID: modID, FileID: fileID, FileName: v.File.Filename}, buf.Bytes(), true, nil
	}
	return curseforge.Match{}, nil, false, nil
}

func (a *app) curseForgeRecords(ctx context.Context, projectIDs []int) (map[int]curseforge.Record, error) {
	cf, err := a.curseForgeLookup()
	if err != nil {
		return nil, err
	}
	a.progress("looking up %s for modlist.html", plural(len(projectIDs), "CurseForge project", "CurseForge projects"))
	return cf.Records(ctx, projectIDs)
}

func (a *app) curseForgeLookup() (*curseforge.CurseForge, error) {
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
	return cf, nil
}
