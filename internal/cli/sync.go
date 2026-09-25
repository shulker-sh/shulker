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
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/saves"
	"shulker.sh/shulker/schema"
)

type syncResult struct {
	Source     string               `json:"source"`
	Kind       modpack.Kind         `json:"kind"`
	Path       string               `json:"path,omitempty"`
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
	// at is the ref and path a sync names with its source, and what a sync without one refuses.
	at                  modpack.At
	side, into, os      string
	force, assumeClient bool
	features            featureFlags
	backup              string
	// rerun is the take-over command for a state file sync can't read; empty names the command that
	// rebuilds the directory.
	rerun string
	// linked is the modpack a link just pointed the instance at.
	linked string
	// keepConflicts is a launch's sync, which keeps the player's side of a conflict rather than
	// failing.
	keepConflicts bool
}

func (a *app) syncCmd() *cobra.Command {
	var req syncRequest
	var sel instanceSelection
	var offline bool
	cmd := &cobra.Command{
		Use:         "sync [project-dir | git-url | manifest-url]",
		Annotations: acts(),
		Short:       "Download and build one side of a project straight into a directory",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOS(req.os); err != nil {
				return err
			}
			req.backup = "sync"
			if !req.force {
				req.rerun = rerunForced(cmd, args)
			}
			if offline {
				d, err := a.deps()
				if err != nil {
					return err
				}
				d.fetch.Offline = true
			}
			if len(args) == 0 {
				if req.into != "" && req.side == "" && req.at == (modpack.At{}) && a.instance == "" && !sel.all && !sel.narrows() {
					res, err := a.syncRecorded(cmd, req)
					if err != nil {
						return err
					}
					return a.printer.Emit(res, res.print)
				}
				if req.into != "" || req.at != (modpack.At{}) {
					return out.Errorf("usage", "--into, --ref and --path need a source; a registered instance already has them")
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
				if err != nil && a.instance != "" {
					entries, err = a.selectProjectDetached(a.instance, sel, err)
				}
				if err != nil {
					return err
				}
				if a.instance == "" && !sel.all {
					picked, err := a.pickInstance(entries)
					if err != nil {
						return err
					}
					entries = []project.InstanceEntry{picked}
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
			if sel.Launcher != "" {
				return out.Errorf("usage", "--launcher narrows -i, --all, or the picker; it doesn't apply to a source")
			}
			req.side = sel.Side
			src, err := a.openSource(cmd.Context(), args[0], req.at)
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
	cmd.Flags().StringVar(&req.at.Ref, "ref", "", "branch, tag, or commit to sync from a git source (default: the remote HEAD)")
	cmd.Flags().StringVar(&req.at.Path, "path", "", "folder of a git source's repository that holds its shulker.json (default: the root)")
	cmd.Flags().StringVar(&req.os, "os", "", "build for this os instead of the detected one: macos, windows, or linux")
	sel.registerWith(cmd, "sync every instance (narrow with --launcher or --side)", "side to build from a source (default: the only declared side); with -i, --all, or the picker, only client or server instances")
	a.registerFailFast(cmd)
	cmd.Flags().BoolVar(&offline, "offline", false, "don't use the network; build from the last successful sync and cached files")
	req.features.register(cmd, "for this run only")
	return cmd
}

func (s syncResult) print(l *out.Lines) {
	if s.Changes != nil {
		s.Changes.printItems(l)
	}
	l.OKInto("synced "+s.Side, s.Dir, reportAside(s.Build))
	rows := reportDetailRows(l, s.Build)
	if row, ok := savesRow(s.Saves); ok {
		rows = append(rows, row)
	}
	l.Tree(rows...)
}

type syncSource struct {
	*modpack.Checkout
	name    string
	project *project.Project
	// isAuthor is a link's answers rather than a source: a project with no directory yet, which the
	// link writes into the instance it creates.
	isAuthor bool
}

func (s *syncSource) isRemote() bool { return s.Kind != modpack.Local }

func (a *app) openSource(ctx context.Context, from string, at modpack.At) (*syncSource, error) {
	co, err := a.checkout(ctx, from, at)
	if err != nil {
		return nil, err
	}
	s := &syncSource{Checkout: co, name: co.Source}
	if co.Kind == modpack.Local {
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
	side, assumed, err := project.SyncSide(p.Manifest, req.side, req.assumeClient)
	if err != nil {
		return syncResult{}, err
	}
	a.warnAssumedClient(assumed)
	remote := src.isRemote()
	var remoteSource string
	if remote {
		remoteSource = src.name
	}
	into, ownBuild, syncedDir, err := project.SyncTarget(p.Manifest, src.Dir, side, req.into, remoteSource)
	if err != nil {
		return syncResult{}, err
	}
	defer func() { a.stampSync(into, err) }()
	lf, inst, err := a.sourceLocalFiles(src, into)
	if err != nil {
		return syncResult{}, err
	}
	fetched, err := a.fetchLocked(ctx, p, nil, side == "server")
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
	overrides, err := req.features.overrides(b, build.MergeDecisions(lf.Features, inst.Features))
	if err != nil {
		return syncResult{}, err
	}
	origin := build.Origin{Source: src.name, Ref: src.Ref, Path: src.Path, Commit: src.Commit, Sha256: src.Sha256}
	rep, err := b.Build(side, build.Options{Force: req.force, Dir: into, NoDataLinks: !ownBuild, OS: req.os, Features: overrides, Origin: origin, BeforeModChange: a.beforeModChange(req.backup, into), KeepConflicts: req.keepConflicts})
	if err != nil {
		return syncResult{}, err
	}
	a.warn(rep.Warnings)
	a.warnKeptConflicts(rep.KeptConflicts, p, side, into)
	a.warnState(rep.State, a.syncTakeOver(req, p, side, into))
	if side == "client" {
		a.syncLauncherImage(into, b)
	}
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
	res = syncResult{Source: src.name, Kind: src.Kind, Path: src.Path, Commit: src.Commit, Sha256: src.Sha256, Offline: src.Offline, Side: side, Dir: into, Fetched: fetched, Build: rep, Saves: linked}
	if !src.LastGood.IsZero() {
		res.LastGoodAt = src.LastGood.Format(time.RFC3339)
	}
	if syncedDir {
		_, _, inPlace, err := project.InPlace(into)
		if err != nil {
			return syncResult{}, err
		}
		if err := instance.SaveIntent(into, inPlace, src.name, src.At, side, req.assumeClient); err != nil {
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
		if i, ok := config.FindInstance(instances, dir); ok {
			instances[i].RecordSync(at, failure)
		}
		return instances
	})
}

func (a *app) stampIntent(dir, at, result string) {
	f, err := instance.Load(dir)
	if errors.Is(err, instance.ErrNotFound) {
		return
	}
	if err == nil {
		f.RecordSync(at, result)
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
	e := project.Inspect(config.Instance{Dir: into})
	if e.Source == "" {
		e := out.Errorf("source-unknown", "%s has no record of what it was synced from", into)
		e.Help = "name the source"
		return syncResult{}, e
	}
	return a.syncInstance(cmd, e, req)
}

// loadLocal reads dir's shulker.local.json, warning and going on with an empty one when it had to
// move an unreadable file aside.
func (a *app) loadLocal(dir string) (*local.File, error) {
	lf, err := local.Load(dir)
	var unreadable *local.UnreadableError
	if errors.As(err, &unreadable) {
		if unreadable.Newer() {
			a.printer.WarnNudge(schema.UpdateNudge, "%v", err)
		} else {
			a.printer.Warn("%v", err)
		}
		return lf, nil
	}
	return lf, err
}

func (a *app) sourceLocalFiles(src *syncSource, into string) (proj, inst *local.File, err error) {
	if inst, err = a.loadLocal(into); err != nil {
		return nil, nil, err
	}
	if src.isRemote() {
		return inst, inst, nil
	}
	if proj, err = a.loadLocal(src.Dir); err != nil {
		return nil, nil, err
	}
	return proj, inst, nil
}

func (a *app) sourceStore() *modpack.Store {
	return &modpack.Store{Cache: a.d.cache, Fetch: a.d.fetch, Log: a.progress, Warn: a.printer.Warn}
}

func (a *app) checkout(ctx context.Context, source string, at modpack.At) (*modpack.Checkout, error) {
	if _, err := a.deps(); err != nil {
		return nil, err
	}
	return a.sourceStore().Checkout(ctx, source, at)
}

// syncLauncherImage keeps a registered instance's picture in its launcher in step with the pack
// icon. An icon it can't use is worth a warning, never a failed sync that would keep the game shut.
func (a *app) syncLauncherImage(dir string, b *build.Builder) {
	in, ok := a.registeredInstance(dir)
	if !ok {
		return
	}
	e := launcher.Find(in.Launcher)
	if e == nil || e.Image == nil {
		return
	}
	icon, err := b.InstanceIcon()
	if err == nil {
		last := build.LoadState(dir).LauncherImage
		var hash string
		if hash, err = e.SyncImage(dir, icon, last); err == nil && hash != last {
			err = build.RecordLauncherImage(dir, hash)
		}
	}
	if err != nil {
		a.printer.Warn("launcher image not updated for %q: %v", in.Label(), err)
	}
}
