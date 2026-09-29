package sync

import (
	"context"
	"errors"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/saves"
)

// AssumeClientWarning is what a sync says when it builds a client the manifest doesn't declare.
const AssumeClientWarning = "shulker.json declares no client; building from shared mods and overrides."

// Request is what a sync is asked for beyond its source.
type Request struct {
	// At is the ref and path a sync names with its source, and what a sync without one refuses.
	At                  modpack.At
	Side, Into, OS      string
	Force, AssumeClient bool
	// With and Without turn features on and off for this run only.
	With, Without []string
	// Backup is the reason an automatic backup of the directory's worlds records before the mods
	// change; empty takes none.
	Backup string
	// Reason is the command an in-place sync's relock records in the history entry.
	Reason string
	// Linked is the modpack a link just pointed the instance at.
	Linked string
	// KeepConflicts is a launch's sync, which keeps the player's side of a conflict rather than
	// failing.
	KeepConflicts bool
}

// Result is what a sync built and recorded.
type Result struct {
	Source     string        `json:"source"`
	Kind       modpack.Kind  `json:"kind"`
	Path       string        `json:"path,omitempty"`
	Commit     string        `json:"commit,omitempty"`
	Sha256     string        `json:"sha256,omitempty"`
	Offline    bool          `json:"offline,omitempty"`
	LastGoodAt string        `json:"lastGoodAt,omitempty"`
	Side       string        `json:"side"`
	Dir        string        `json:"dir"`
	Fetched    []string      `json:"fetched"`
	Build      *build.Report `json:"build"`
	Saves      *saves.Result `json:"saves,omitempty"`
	// Project is the one the side was built from.
	Project *project.Project `json:"-"`
	// Relock is what an in-place sync's relock did; nil for a sync that relocked nothing.
	Relock *resolve.Relocked `json:"-"`
}

// Run builds one side of src into the directory the request or the source decides, fetching
// what the lock names first and recording the sync afterwards.
func Run(ctx context.Context, e *Env, src *Source, req Request) (res Result, err error) {
	p := src.Project
	side, assumed, err := project.SyncSide(p.Manifest, req.Side, req.AssumeClient)
	if err != nil {
		return Result{}, err
	}
	if assumed {
		e.Warn(AssumeClientWarning)
	}
	remote := src.IsRemote()
	var remoteSource string
	if remote {
		remoteSource = src.Name
	}
	into, ownBuild, syncedDir, err := project.SyncTarget(p.Manifest, src.Dir, side, req.Into, remoteSource)
	if err != nil {
		return Result{}, err
	}
	defer func() { e.stampSync(into, err) }()
	lf, inst, err := SourceLocalFiles(e, src, into)
	if err != nil {
		return Result{}, err
	}
	if remote {
		if err := build.CheckProvenance(e.Providers, p.Manifest, p.Lock, nil, src.Name); err != nil {
			return Result{}, err
		}
		if young := resolve.YoungEntries(p.Lock, e.MinReleaseAge, env.Clock(e.Now)); len(young) > 0 {
			e.WarnSecurity(resolve.YoungWarning(young, e.MinReleaseAge))
		}
	}
	fetched, err := FetchLocked(ctx, e, p, nil, side == "server")
	if err != nil {
		return Result{}, err
	}
	if err := Players(ctx, e, p, player.MissingOnly, false, !remote); err != nil {
		return Result{}, err
	}
	b, err := e.builder(ctx, p)
	if err != nil {
		return Result{}, err
	}
	overrides, err := FeatureOverrides(b, req.With, req.Without, build.MergeDecisions(lf.Features, inst.Features))
	if err != nil {
		return Result{}, err
	}
	checked := e.checkTakedowns(ctx, into, p.Lock, src.Offline)
	origin := instance.Origin{Source: src.Name, Ref: src.Ref, Path: src.Path, Commit: src.Commit, Sha256: src.Sha256}
	rep, err := b.Build(side, build.Options{Force: req.Force, Dir: into, NoDataLinks: !ownBuild, OS: req.OS, Features: overrides, Origin: origin, BeforeModChange: e.beforeModChange(req.Backup, into), KeepConflicts: req.KeepConflicts, Takedowns: checked})
	if err != nil {
		return Result{}, err
	}
	e.WarnEach(rep.Warnings)
	for _, w := range rep.SecurityWarnings(e.warnContext(p, side, into)) {
		e.WarnSecurity(w)
	}
	if side == "client" {
		e.syncLauncherImage(into, b)
	}
	if err := InstallServerLoader(ctx, e, p, rep); err != nil {
		return Result{}, err
	}
	rt, err := e.recordClientRuntime(ctx, p, side, into)
	if err != nil {
		return Result{}, err
	}
	if rt.Fetched {
		fetched = append(fetched, rt.Component+" "+rt.Version)
	}
	linked, err := e.linkSaves(into)
	if err != nil {
		return Result{}, err
	}
	recorded := !remote && !ownBuild && lf.RecordSyncDir(side, into)
	RefreshLocal(e, lf, !remote, recorded)
	if !remote {
		RefreshLocal(e, inst, false, false)
	}
	if remote && !src.Offline {
		if err := e.Store().RecordGood(src.Checkout); err != nil {
			e.Warn("couldn't record %s as the offline fallback: %v.", src.Name, err)
		}
	}
	res = Result{Source: src.Name, Kind: src.Kind, Path: src.Path, Commit: src.Commit, Sha256: src.Sha256, Offline: src.Offline, Side: side, Dir: into, Fetched: fetched, Build: rep, Saves: linked, Project: p}
	if !src.LastGood.IsZero() {
		res.LastGoodAt = src.LastGood.Format(time.RFC3339)
	}
	if syncedDir {
		_, _, inPlace, err := project.InPlace(into)
		if err != nil {
			return Result{}, err
		}
		if err := instance.SaveIntent(into, inPlace, src.Name, src.At, side, req.AssumeClient); err != nil {
			return Result{}, err
		}
		e.refreshRegistered(config.Instance{Dir: into, Source: src.Name})
	}
	return res, nil
}

// SourceLocalFiles is the local file of the source and of the directory synced into; a remote
// source has none of its own, so the directory's stands for both.
func SourceLocalFiles(e *Env, src *Source, into string) (proj, inst *local.File, err error) {
	if inst, err = LoadLocal(e, into); err != nil {
		return nil, nil, err
	}
	if src.IsRemote() {
		return inst, inst, nil
	}
	if proj, err = LoadLocal(e, src.Dir); err != nil {
		return nil, nil, err
	}
	return proj, inst, nil
}

// stampSync records how a sync of a registered instance ended, in the instance file and on its
// registry row. A failed sync leaves lastSyncAt and lastSync where they are: the files in the
// directory are still the ones the last good sync built.
func (e *Env) stampSync(dir string, syncErr error) {
	if _, ok := e.registered(dir); !ok {
		return
	}
	at := time.Now().UTC().Format(time.RFC3339)
	result, failure := instance.ResultOK, ""
	if syncErr != nil {
		result, failure = instance.ResultFailed, out.AsError(syncErr).Message
	}
	e.stampIntent(dir, at, result)
	e.updateInstances(func(instances []config.Instance) []config.Instance {
		if i, ok := config.FindInstance(instances, dir); ok {
			instances[i].RecordSync(at, failure)
		}
		return instances
	})
}

func (e *Env) stampIntent(dir, at, result string) {
	f, err := instance.Load(dir)
	if errors.Is(err, instance.ErrNotFound) {
		return
	}
	if err == nil {
		f.RecordSync(at, result)
		err = f.Save(dir)
	}
	if err != nil {
		e.Warn("%s not updated: %v", instance.Path(dir), out.AsError(err).Message)
	}
}
