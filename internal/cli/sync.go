package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
)

type syncResult struct {
	Source   string        `json:"source"`
	Kind     pack.Kind     `json:"kind"`
	Commit   string        `json:"commit,omitempty"`
	Target   string        `json:"target"`
	Dir      string        `json:"dir"`
	Fetched  []string      `json:"fetched"`
	Warnings []string      `json:"warnings"`
	Build    *build.Report `json:"build"`
}

func (a *app) syncCmd() *cobra.Command {
	var target, into, ref, osName string
	var force bool
	var ff featureFlags
	cmd := &cobra.Command{
		Use:   "sync <project-dir | git-url | manifest-url>",
		Short: "Download and build one target of a project straight into a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(osName); err != nil {
				return err
			}
			co, err := a.checkout(cmd.Context(), args[0], ref)
			if err != nil {
				return err
			}
			source := co.Source
			if co.Kind == pack.Local {
				source = co.Dir
			}
			p, err := a.openProjectAt(co.Dir)
			if errors.Is(err, project.ErrNoManifest) {
				return out.Errorf("project-not-found", "no shulker.json in %s", source)
			}
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale {
				a.progress("warning: shulker.lock is out of date with shulker.json; run `shulker add`, `remove`, or `update` to refresh it")
			}
			name, t, err := singleTarget(p, target)
			if err != nil {
				return err
			}
			remote := co.Kind != pack.Local
			if into == "" && remote {
				return out.Errorf("into-required", "--into is required when syncing from %s", source)
			}
			buildDir, err := filepath.Abs(filepath.Join(co.Dir, p.Manifest.BuildDir(name)))
			if err != nil {
				return err
			}
			if into == "" {
				into = buildDir
			}
			if into, err = filepath.Abs(into); err != nil {
				return err
			}
			localDir := co.Dir
			if remote {
				localDir = into
			}
			lf, err := local.Load(localDir)
			if err != nil {
				return err
			}
			fetched, warnings, err := a.fetchLocked(cmd.Context(), p, t.Side == "server")
			if err != nil {
				return err
			}
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false, !remote); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			overrides, err := featureOverrides(b, lf.Features, ff)
			if err != nil {
				return err
			}
			rep, err := b.Build(name, build.Options{Force: force, Dir: into, NoDataLinks: remote, OS: osName, Features: overrides})
			if err != nil {
				return err
			}
			a.warn(rep.Warnings)
			recorded := !remote && into != buildDir && lf.RecordSyncDir(name, into)
			a.refreshLocal(lf, !remote, recorded)
			res := syncResult{Source: source, Kind: co.Kind, Commit: co.Commit, Target: name, Dir: into, Fetched: fetched, Warnings: warnings, Build: rep}
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintf(w, "fetched %d file(s)\n", len(res.Fetched))
				fmt.Fprintf(w, "%s into %s\n", rep.Summary(), into)
				printReportDetails(w, rep)
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target to build (default: the only target)")
	cmd.Flags().StringVar(&into, "into", "", "output directory (default: the target's build directory)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the output directory")
	cmd.Flags().StringVar(&ref, "ref", "", "branch, tag, or commit to sync from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&osName, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	ff.register(cmd, "for this run only")
	return cmd
}

func (a *app) checkout(ctx context.Context, source, ref string) (*pack.Checkout, error) {
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	store := &pack.Store{CacheDir: d.cache.Dir, Fetch: d.fetch, Log: a.progress}
	return store.Checkout(ctx, source, ref)
}

func singleTarget(p *project.Project, want string) (string, manifest.Target, error) {
	if want != "" {
		t, ok := p.Manifest.Targets[want]
		if !ok {
			e := out.Errorf("target-not-found", "no target %q in shulker.json", want)
			e.Candidates = targetNames(p.Manifest.Targets)
			return "", t, e
		}
		return want, t, nil
	}
	names := targetNames(p.Manifest.Targets)
	if len(names) == 1 {
		return names[0], p.Manifest.Targets[names[0]], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several targets; pass --target")
	e.Candidates = names
	return "", manifest.Target{}, e
}
