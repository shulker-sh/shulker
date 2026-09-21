package cli

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/saves"
)

type syncResult struct {
	Source     string               `json:"source"`
	Kind       pack.Kind            `json:"kind"`
	Commit     string               `json:"commit,omitempty"`
	Sha256     string               `json:"sha256,omitempty"`
	Offline    bool                 `json:"offline,omitempty"`
	LastGoodAt string               `json:"lastGoodAt,omitempty"`
	Side       string               `json:"side"`
	Dir        string               `json:"dir"`
	Fetched    []string             `json:"fetched"`
	Build      *build.Report        `json:"build"`
	Changes    *lockChanges         `json:"changes,omitempty"`
	Instances  []syncInstanceResult `json:"instances,omitempty"`
	Saves      *saves.Result        `json:"saves,omitempty"`
}

type syncRequest struct {
	ref, side, into, os string
	force, assumeClient bool
	features            featureFlags
	backup              string
}

func (a *app) syncCmd() *cobra.Command {
	var req syncRequest
	var sel instanceSelection
	var offline bool
	cmd := &cobra.Command{
		Use:   "sync [project-dir | git-url | manifest-url]",
		Short: "Download and build one side of a project straight into a directory",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(req.os); err != nil {
				return err
			}
			req.backup = "sync"
			if offline {
				d, err := a.deps()
				if err != nil {
					return err
				}
				d.fetch.Offline = true
			}
			if len(args) == 0 {
				if req.into != "" && req.side == "" && req.ref == "" && a.instance == "" && !sel.all && !sel.narrows() {
					res, err := a.syncRecorded(cmd, req)
					if err != nil {
						return err
					}
					return a.printer.Emit(res, res.print)
				}
				if req.into != "" || req.ref != "" {
					return out.Errorf("usage", "--into and --ref need a source; a registered instance already has them")
				}
				if a.instance == "" && !sel.all {
					dir, err := a.scopeDir()
					if err != nil {
						return err
					}
					if p, side, ok, err := a.inPlaceProject(dir); err != nil {
						return err
					} else if ok {
						return a.syncTree(cmd, p, side, sel, req)
					}
					entries, inProject, err := a.projectInstances(sel)
					if err != nil {
						return err
					}
					if inProject {
						return a.syncInstances(cmd, entries, req)
					}
				}
				entries, err := a.selectInstances(a.instance, sel)
				if err != nil {
					return err
				}
				if a.instance == "" && !sel.all {
					picked, err := a.pickInstance(entries)
					if err != nil {
						return err
					}
					entries = []instanceEntry{picked}
				}
				if sel.all {
					return a.syncInstances(cmd, entries, req)
				}
				res, err := a.syncInstance(cmd, entries[0], req)
				if err != nil {
					return err
				}
				return a.printer.Emit(res, res.print)
			}
			if a.instance != "" || sel.all {
				return out.Errorf("usage", "pass a source or -i/--all, not both")
			}
			if sel.launcher != "" {
				return out.Errorf("usage", "--launcher narrows -i, --all, or the picker; it doesn't apply to a source")
			}
			req.side = sel.side
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
	cmd.Flags().StringVar(&req.into, "into", "", "output directory (default: the side's build directory)")
	cmd.Flags().BoolVar(&req.force, "force", false, "overwrite files edited in the output directory")
	cmd.Flags().BoolVar(&req.assumeClient, "assume-client", false, "build a client even when the source declares none, from the mods and overrides both sides share")
	cmd.Flags().StringVar(&req.ref, "ref", "", "branch, tag, or commit to sync from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&req.os, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	sel.registerWith(cmd, "sync every instance (narrow with --launcher or --side)", "side to build from a source (default: the only declared side); with -i, --all, or the picker, only client or server instances")
	cmd.Flags().BoolVar(&offline, "offline", false, "don't use the network; build from the last successful sync and cached files")
	req.features.register(cmd, "for this run only")
	return cmd
}

func (res syncResult) print(l *out.Lines) {
	if res.Changes != nil {
		res.Changes.printItems(l)
	}
	l.OKInto("synced "+res.Side, res.Dir, reportAside(res.Build))
	rows := reportDetailRows(l, res.Build)
	if row, ok := savesRow(res.Saves); ok {
		rows = append(rows, row)
	}
	l.Tree(rows...)
}

type syncSource struct {
	*pack.Checkout
	name    string
	project *project.Project
	// author is a link's answers rather than a source: a project with no directory yet, which the
	// link writes into the instance it creates.
	author bool
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
		a.printer.Drop()
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

func (a *app) sync(ctx context.Context, src *syncSource, req syncRequest) (res syncResult, err error) {
	p := src.project
	side, err := a.syncSide(p, req.side, req.assumeClient)
	if err != nil {
		return syncResult{}, err
	}
	remote := src.remote()
	into := req.into
	if into == "" && remote {
		return syncResult{}, out.Errorf("into-required", "--into is required when syncing from %s", src.name)
	}
	buildDir, err := filepath.Abs(filepath.Join(src.Dir, p.Manifest.BuildDir(side)))
	if err != nil {
		return syncResult{}, err
	}
	if into == "" {
		into = buildDir
	}
	if into, err = filepath.Abs(into); err != nil {
		return syncResult{}, err
	}
	defer func() { a.stampSync(into, err) }()
	ownBuild := sameDir(into, buildDir)
	syncedDir := req.into != "" && !ownBuild
	lf, inst, err := sourceLocalFiles(src, into)
	if err != nil {
		return syncResult{}, err
	}
	fetched, err := a.fetchLocked(ctx, p, side == "server")
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
	origin := build.Origin{Source: src.name, Ref: req.ref, Commit: src.Commit, Sha256: src.Sha256}
	rep, err := b.Build(side, build.Options{Force: req.force, Dir: into, NoDataLinks: !ownBuild, OS: req.os, Features: overrides, Origin: origin, BeforeModChange: a.autoBackup(req.backup, into)})
	if err != nil {
		return syncResult{}, err
	}
	a.warn(rep.Warnings)
	if err := a.installServerLoader(ctx, p, rep); err != nil {
		return syncResult{}, err
	}
	if rt, err := a.recordClientRuntime(ctx, p, side, into); err != nil {
		return syncResult{}, err
	} else if rt.Fetched {
		fetched = append(fetched, rt.Component+" "+rt.Version)
	}
	linked, err := a.linkSaves(into)
	if err != nil {
		return syncResult{}, err
	}
	recorded := !remote && !ownBuild && lf.RecordSyncDir(side, into)
	a.refreshLocal(lf, !remote, recorded)
	if !remote {
		a.refreshLocal(inst, false, false)
	}
	if remote && !src.Offline {
		if err := a.sourceStore().RecordGood(src.Checkout); err != nil {
			a.printer.Warn("couldn't record %s as the offline fallback: %v", src.name, err)
		}
	}
	res = syncResult{Source: src.name, Kind: src.Kind, Commit: src.Commit, Sha256: src.Sha256, Offline: src.Offline, Side: side, Dir: into, Fetched: fetched, Build: rep, Saves: linked}
	if !src.LastGood.IsZero() {
		res.LastGoodAt = src.LastGood.Format(time.RFC3339)
	}
	if syncedDir {
		if err := saveIntent(into, src.name, req.ref, side, req.assumeClient); err != nil {
			return syncResult{}, err
		}
		a.refreshRegistered(config.Instance{Dir: into, Source: src.name})
	}
	return res, nil
}

// stampSync records how a sync of a registered instance ended, in the instance file and on its
// registry row. A failed sync leaves lastSyncAt and lastSync where they are: the files in the
// directory are still the ones the last good sync built.
func (a *app) stampSync(dir string, syncErr error) {
	if _, ok := a.registeredInstance(dir); !ok {
		return
	}
	at := time.Now().UTC().Format(time.RFC3339)
	result, failure := instance.ResultOK, ""
	if syncErr != nil {
		result, failure = instance.ResultFailed, out.AsError(syncErr).Message
	}
	a.stampIntent(dir, at, result)
	a.updateInstances(func(instances []config.Instance) []config.Instance {
		i, ok := config.FindInstance(instances, dir)
		if !ok {
			return instances
		}
		if syncErr == nil {
			instances[i].LastSync = at
		}
		instances[i].LastError = failure
		return instances
	})
}

func (a *app) stampIntent(dir, at, result string) {
	f, err := instance.Load(dir)
	if errors.Is(err, instance.ErrNotFound) {
		return
	}
	if err == nil {
		if f.Resolved == nil {
			f.Resolved = &instance.Resolved{}
		}
		if result == instance.ResultOK {
			f.Resolved.LastSyncAt = at
		}
		f.Resolved.LastResult = result
		err = f.Save(dir)
	}
	if err != nil {
		a.printer.Warn("%s not updated: %v", instance.Path(dir), out.AsError(err).Message)
	}
}

// syncRecorded syncs a directory from the source its own instance file records, so a synced
// directory stays usable after the registry is gone.
func (a *app) syncRecorded(cmd *cobra.Command, req syncRequest) (syncResult, error) {
	into, err := filepath.Abs(req.into)
	if err != nil {
		return syncResult{}, err
	}
	e := inspectInstance(config.Instance{Dir: into})
	if e.Source == "" {
		return syncResult{}, out.Errorf("source-unknown", "%s has no record of what it was synced from; name the source", into)
	}
	return a.syncInstance(cmd, e, req)
}

// sameDir also treats a symlink to dir as dir, since a launcher's game directory may be reached
// through one.
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
