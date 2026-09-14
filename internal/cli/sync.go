package cli

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
)

type syncResult struct {
	Source     string        `json:"source"`
	Kind       pack.Kind     `json:"kind"`
	Commit     string        `json:"commit,omitempty"`
	Sha256     string        `json:"sha256,omitempty"`
	Offline    bool          `json:"offline,omitempty"`
	LastGoodAt string        `json:"lastGoodAt,omitempty"`
	Target     string        `json:"target"`
	Dir        string        `json:"dir"`
	Fetched    []string      `json:"fetched"`
	Build      *build.Report `json:"build"`
	Registered *config.Link  `json:"registered,omitempty"`
}

type syncRequest struct {
	ref, target, into, os, name string
	force                       bool
	features                    featureFlags
}

func (a *app) syncCmd() *cobra.Command {
	var req syncRequest
	var instance string
	var sel linkSelection
	var offline bool
	cmd := &cobra.Command{
		Use:   "sync [project-dir | git-url | manifest-url]",
		Short: "Download and build one target of a project straight into a directory",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(req.os); err != nil {
				return err
			}
			if offline {
				d, err := a.deps()
				if err != nil {
					return err
				}
				d.fetch.Offline = true
			}
			if len(args) == 0 {
				if req.into != "" && req.target == "" && req.ref == "" && req.name == "" && instance == "" && !sel.all && !sel.narrows() {
					res, err := a.syncRecorded(cmd, req)
					if err != nil {
						return err
					}
					return a.printer.Emit(res, res.print)
				}
				if req.target != "" || req.into != "" || req.ref != "" || req.name != "" {
					return out.Errorf("usage", "--target, --into, --ref, and --name need a source; a registered entry already has them")
				}
				if instance == "" && !sel.all {
					links, inProject, err := a.projectLinks(sel)
					if err != nil {
						return err
					}
					if inProject {
						return a.syncLinks(cmd, links, req)
					}
				}
				links, err := a.selectLinks(instance, sel)
				if err != nil {
					return err
				}
				if instance == "" && !sel.all {
					l, err := a.pickLink(links)
					if err != nil {
						return err
					}
					links = []config.Link{l}
				}
				if sel.all {
					return a.syncLinks(cmd, links, req)
				}
				res, err := a.syncLink(cmd, links[0], req)
				if err != nil {
					return err
				}
				return a.printer.Emit(res, res.print)
			}
			if instance != "" || sel.all {
				return out.Errorf("usage", "pass a source or --instance/--all, not both")
			}
			if sel.narrows() {
				return out.Errorf("usage", "--launcher and --side narrow --instance, --all, or the picker; they don't apply to a source")
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
	cmd.Flags().StringVar(&req.name, "name", "", "name to register the --into directory under (default: the target's display name)")
	cmd.Flags().StringVar(&instance, "instance", "", "sync a linked instance or synced directory, by name or directory, from its recorded source")
	sel.register(cmd, "sync every linked instance and synced directory (narrow with --launcher or --side)")
	cmd.Flags().BoolVar(&offline, "offline", false, "don't use the network; build from the last successful sync and cached files")
	req.features.register(cmd, "for this run only")
	return cmd
}

func (res syncResult) print(l *out.Lines) {
	l.OKInto("synced "+res.Target, res.Dir, reportAside(res.Build))
	printReportDetails(l, res.Build)
	if r := res.Registered; r != nil {
		l.OK("registered "+r.Name, r.Side)
	}
}

type syncSource struct {
	*pack.Checkout
	name    string
	project *project.Project
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
		a.printer.Warn("%s", co.Warning)
	}
	s.project, err = a.openProjectAt(co.Dir)
	if errors.Is(err, project.ErrNoManifest) {
		return nil, out.Errorf("manifest-not-found", "no shulker.json in %s", s.name)
	}
	if err != nil {
		return nil, err
	}
	if err := a.requireLock(s.project); err != nil {
		return nil, err
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
	ownBuild := sameDir(into, buildDir)
	register := req.into != "" && !ownBuild
	if req.name != "" && !register {
		return syncResult{}, out.Errorf("usage", "--name needs --into a directory other than the build directory")
	}
	lf, inst, err := sourceLocalFiles(src, into)
	if err != nil {
		return syncResult{}, err
	}
	fetched, err := a.fetchLocked(ctx, p, t.Side == "server")
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
	rep, err := b.Build(name, build.Options{Force: req.force, Dir: into, NoDataLinks: !ownBuild, OS: req.os, Features: overrides, Origin: origin})
	if err != nil {
		return syncResult{}, err
	}
	a.warn(rep.Warnings)
	if err := a.installServerLoader(ctx, p, rep); err != nil {
		return syncResult{}, err
	}
	recorded := !remote && !ownBuild && lf.RecordSyncDir(name, into)
	a.refreshLocal(lf, !remote, recorded)
	if !remote {
		a.refreshLocal(inst, false, false)
	}
	if remote && !src.Offline {
		if err := a.sourceStore().RecordGood(src.Checkout); err != nil {
			a.printer.Warn("couldn't record %s as the offline fallback: %v", src.name, err)
		}
	}
	res := syncResult{Source: src.name, Kind: src.Kind, Commit: src.Commit, Sha256: src.Sha256, Offline: src.Offline, Target: name, Dir: into, Fetched: fetched, Build: rep}
	if !src.LastGood.IsZero() {
		res.LastGoodAt = src.LastGood.Format(time.RFC3339)
	}
	if register {
		entry := config.Link{Side: t.Side, Name: req.name, Dir: into, Source: src.name, Target: name, Ref: req.ref}
		if entry, changed := a.registerSync(entry, p.Manifest.DisplayName(name)); changed {
			res.Registered = &entry
		}
	}
	return res, nil
}

// syncRecorded syncs a directory from the source its own state file records, so
// a synced directory stays usable after the links registry is gone.
func (a *app) syncRecorded(cmd *cobra.Command, req syncRequest) (syncResult, error) {
	into, err := filepath.Abs(req.into)
	if err != nil {
		return syncResult{}, err
	}
	st, stateErr := build.ReadState(into)
	if stateErr != nil {
		a.printer.Warn("%v", stateErr)
	}
	if st.Source == "" {
		return syncResult{}, out.Errorf("source-unknown", "%s has no record of what it was synced from; name the source", into)
	}
	return a.syncLink(cmd, config.Link{Dir: into, Source: st.Source, Ref: st.Ref, Target: st.Target}, req)
}

// sameDir also treats a symlink to dir as dir, e.g. a Prism instance linked in symlink mode.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
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

func (a *app) sourceStore() *pack.Store {
	return &pack.Store{Cache: a.d.cache, Fetch: a.d.fetch, Log: a.progress}
}

func (a *app) checkout(ctx context.Context, source, ref string) (*pack.Checkout, error) {
	if _, err := a.deps(); err != nil {
		return nil, err
	}
	return a.sourceStore().Checkout(ctx, source, ref)
}

func singleTarget(p *project.Project, want string) (string, manifest.Target, error) {
	if want != "" {
		t, err := p.Manifest.Target(want)
		if err != nil {
			return "", t, err
		}
		return want, t, nil
	}
	names := targetNames(p.Manifest.Targets)
	if len(names) == 1 {
		return names[0], p.Manifest.Targets[names[0]], nil
	}
	e := out.Errorf("ambiguous-target", "shulker.json has several targets; pass --target")
	e.Candidates, e.Flag = names, "--target"
	return "", manifest.Target{}, e
}
