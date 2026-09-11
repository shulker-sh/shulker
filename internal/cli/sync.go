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

type syncRequest struct {
	ref, target, into, os string
	force                 bool
	features              featureFlags
}

func (a *app) syncCmd() *cobra.Command {
	var req syncRequest
	cmd := &cobra.Command{
		Use:   "sync <project-dir | git-url | manifest-url>",
		Short: "Download and build one target of a project straight into a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(req.os); err != nil {
				return err
			}
			src, err := a.openSource(cmd.Context(), args[0], req.ref)
			if err != nil {
				return err
			}
			res, err := a.sync(cmd.Context(), src, req)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, res.print)
		},
	}
	cmd.Flags().StringVar(&req.target, "target", "", "target to build (default: the only target)")
	cmd.Flags().StringVar(&req.into, "into", "", "output directory (default: the target's build directory)")
	cmd.Flags().BoolVar(&req.force, "force", false, "overwrite files edited in the output directory")
	cmd.Flags().StringVar(&req.ref, "ref", "", "branch, tag, or commit to sync from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&req.os, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	req.features.register(cmd, "for this run only")
	return cmd
}

func (res syncResult) print(w io.Writer) {
	fmt.Fprintf(w, "fetched %d file(s)\n", len(res.Fetched))
	fmt.Fprintf(w, "%s into %s\n", res.Build.Summary(), res.Dir)
	printReportDetails(w, res.Build)
}

type syncSource struct {
	*pack.Checkout
	name     string
	project  *project.Project
	warnings []string
}

func (s *syncSource) remote() bool { return s.Kind != pack.Local }

func (a *app) openSource(ctx context.Context, from, ref string) (*syncSource, error) {
	co, err := a.checkout(ctx, from, ref)
	if err != nil {
		return nil, err
	}
	s := &syncSource{Checkout: co, name: co.Source}
	if co.Kind == pack.Local {
		s.name = co.Dir
	}
	if co.Warning != "" {
		s.warnings = []string{co.Warning}
		a.warn(s.warnings)
	}
	s.project, err = a.openProjectAt(co.Dir)
	if errors.Is(err, project.ErrNoManifest) {
		return nil, out.Errorf("project-not-found", "no shulker.json in %s", s.name)
	}
	if err != nil {
		return nil, err
	}
	if err := s.project.RequireLock(); err != nil {
		return nil, err
	}
	if a.printer.LockStale {
		a.progress("warning: shulker.lock is out of date with shulker.json; run `shulker add`, `remove`, or `update` to refresh it")
	}
	return s, nil
}

func (a *app) sync(ctx context.Context, src *syncSource, req syncRequest) (syncResult, error) {
	p := src.project
	name, t, err := singleTarget(p, req.target)
	if err != nil {
		return syncResult{}, err
	}
	remote := src.remote()
	into := req.into
	if into == "" && remote {
		return syncResult{}, out.Errorf("into-required", "--into is required when syncing from %s", src.name)
	}
	buildDir, err := filepath.Abs(filepath.Join(src.Dir, p.Manifest.BuildDir(name)))
	if err != nil {
		return syncResult{}, err
	}
	if into == "" {
		into = buildDir
	}
	if into, err = filepath.Abs(into); err != nil {
		return syncResult{}, err
	}
	lf, inst, err := sourceLocalFiles(src, into)
	if err != nil {
		return syncResult{}, err
	}
	fetched, warnings, err := a.fetchLocked(ctx, p, t.Side == "server")
	if err != nil {
		return syncResult{}, err
	}
	if err := a.syncPlayers(ctx, p, player.MissingOnly, false, !remote); err != nil {
		return syncResult{}, err
	}
	b, err := a.builder(ctx, p)
	if err != nil {
		return syncResult{}, err
	}
	overrides, err := featureOverrides(b, mergeDecisions(lf.Features, inst.Features), req.features)
	if err != nil {
		return syncResult{}, err
	}
	origin := build.Origin{Source: src.name, Ref: req.ref, Commit: src.Commit}
	rep, err := b.Build(name, build.Options{Force: req.force, Dir: into, NoDataLinks: remote, OS: req.os, Features: overrides, Origin: origin})
	if err != nil {
		return syncResult{}, err
	}
	a.warn(rep.Warnings)
	recorded := !remote && into != buildDir && lf.RecordSyncDir(name, into)
	a.refreshLocal(lf, !remote, recorded)
	if !remote {
		a.refreshLocal(inst, false, false)
	}
	return syncResult{Source: src.name, Kind: src.Kind, Commit: src.Commit, Target: name, Dir: into, Fetched: fetched, Warnings: append(src.warnings, warnings...), Build: rep}, nil
}

func sourceLocalFiles(src *syncSource, into string) (proj, inst *local.File, err error) {
	if inst, err = local.Load(into); err != nil {
		return nil, nil, err
	}
	if src.remote() {
		return inst, inst, nil
	}
	if proj, err = local.Load(src.Dir); err != nil {
		return nil, nil, err
	}
	return proj, inst, nil
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
